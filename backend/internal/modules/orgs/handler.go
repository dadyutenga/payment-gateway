package orgs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, status int, code, message string, err error) {
	if err != nil {
		h.logger.Error("request failed", "code", code, "status", status, "error", err)
	}
	httputil.Error(w, status, code, message, nil)
}

func claimsUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return "", false
	}
	return claims.Subject, true
}

// claimsIdentity returns user id + email for admin actions that record
// who decided (KYC review). Route-level RequireAdmin gates access.
func claimsIdentity(w http.ResponseWriter, r *http.Request) (userID, email string, ok bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return "", "", false
	}
	return claims.Subject, claims.Email, true
}

// orgError maps domain errors to JSON responses — always a clear body,
// never a raw Go error.
func (h *Handler) orgError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, ErrOrgNotFound):
		httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
	case errors.Is(err, ErrNotOrgMember):
		httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this organization.", nil)
	case errors.Is(err, ErrForbidden):
		httputil.Error(w, http.StatusForbidden, "forbidden", "Your role doesn't allow "+action+".", nil)
	case errors.Is(err, ErrAlreadyMember):
		httputil.Error(w, http.StatusConflict, "already_member", "This user is already a member of this organization.", nil)
	case errors.Is(err, ErrUserNotFound):
		httputil.Error(w, http.StatusNotFound, "user_not_found", "No account found for that email — they need to sign up first.", nil)
	case errors.Is(err, ErrLastOwner):
		httputil.Error(w, http.StatusConflict, "last_owner", "The organization must keep at least one owner.", nil)
	case errors.Is(err, ErrOrgNotEmpty):
		httputil.Error(w, http.StatusConflict, "org_not_empty", "Delete or move the organization's apps first.", nil)
	case errors.Is(err, ErrInviteNotFound):
		httputil.Error(w, http.StatusNotFound, "invite_not_found", "No pending invite for this user.", nil)
	case errors.Is(err, ErrCannotRemoveSelf):
		httputil.Error(w, http.StatusBadRequest, "cannot_remove_self", "Use leave instead of removing yourself.", nil)
	case errors.Is(err, ErrSingleOrg):
		httputil.Error(w, http.StatusConflict, "single_org", "Each account belongs to a single organization.", nil)
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to complete the organization request.", err)
	}
}

type createOrgHTTPInput struct {
	Name         string `json:"name"`
	BusinessName string `json:"business_name"`
}

func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in createOrgHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	org, vErrs, err := h.service.CreateOrganization(r.Context(), userID, in.Name, in.BusinessName)
	if err != nil {
		if errors.Is(err, ErrSingleOrg) {
			httputil.Error(w, http.StatusConflict, "single_org", "Each account belongs to a single organization.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your organization input.", vErrs)
		return
	}
	member, err := h.service.CheckOrgPermission(r.Context(), userID, org.ID, PermRead)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": OrganizationWithRole{Organization: org, Role: member.Role, Status: member.Status}})
}

func (h *Handler) ListMyOrganizations(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgs, err := h.service.ListMyOrganizations(r.Context(), userID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "list_failed", "Unable to list organizations.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": orgs})
}

func (h *Handler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	org, err := h.service.GetOrganization(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "view this organization")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

type updateOrgHTTPInput struct {
	Name         string `json:"name"`
	BusinessName string `json:"business_name"`
	TIN          string `json:"tin"`
	Address      string `json:"address"`
	Phone        string `json:"phone"`
	ContactEmail string `json:"contact_email"`
	LogoURL      string `json:"logo_url"`
	PrimaryColor string `json:"primary_color"`
}

func (h *Handler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in updateOrgHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	org, vErrs, err := h.service.UpdateOrganization(r.Context(), userID, r.PathValue("orgID"), OrgProfileUpdate{
		Name: in.Name, BusinessName: in.BusinessName, TIN: in.TIN,
		Address: in.Address, Phone: in.Phone, ContactEmail: in.ContactEmail,
		LogoURL: in.LogoURL, PrimaryColor: in.PrimaryColor,
	})
	if err != nil {
		if errors.Is(err, ErrReverificationRequired) {
			httputil.Error(w, http.StatusConflict, "reverification_required", "Business name and TIN are locked after verification — resubmit verification to change them.", nil)
			return
		}
		h.orgError(w, err, "manage this organization")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your organization input.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

func (h *Handler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteOrganization(r.Context(), userID, r.PathValue("orgID")); err != nil {
		h.orgError(w, err, "delete this organization")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	members, err := h.service.ListMembers(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "view members")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": members})
}

type inviteMemberHTTPInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) InviteMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in inviteMemberHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	role, err := ParseRole(strings.ToLower(strings.TrimSpace(in.Role)))
	if err != nil {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Role must be owner, finance, developer, or viewer.", nil)
		return
	}
	member, err := h.service.InviteMember(r.Context(), userID, r.PathValue("orgID"), strings.TrimSpace(in.Email), role)
	if err != nil {
		h.orgError(w, err, "invite members")
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": member})
}

func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	member, err := h.service.AcceptInvite(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "accept this invite")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": member})
}

type changeRoleHTTPInput struct {
	Role string `json:"role"`
}

func (h *Handler) ChangeMemberRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in changeRoleHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	role, err := ParseRole(strings.ToLower(strings.TrimSpace(in.Role)))
	if err != nil {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Role must be owner, finance, developer, or viewer.", nil)
		return
	}
	member, err := h.service.ChangeMemberRole(r.Context(), userID, r.PathValue("orgID"), r.PathValue("userID"), role)
	if err != nil {
		h.orgError(w, err, "change member roles")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": member})
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	if err := h.service.RemoveMember(r.Context(), userID, r.PathValue("orgID"), r.PathValue("userID")); err != nil {
		h.orgError(w, err, "remove members")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LeaveOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	if err := h.service.LeaveOrganization(r.Context(), userID, r.PathValue("orgID")); err != nil {
		h.orgError(w, err, "leave this organization")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxKYCDocumentBytes caps ID uploads at 5MB.
const maxKYCDocumentBytes = 5 << 20

// kycUploadDir is relative to the process working directory (backend/ for
// `go run ./cmd/api`). No object storage exists in this repo yet — when an
// S3-compatible store is introduced, only saveKYCDocument/serveKYCDocument
// need to change (the DB keeps a location string either way).
const kycUploadDir = "uploads/kyc"

var allowedKYCExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".pdf": true,
}

func kycContentTypeAllowed(detected, filename string) bool {
	switch detected {
	case "image/jpeg", "image/png", "image/webp", "application/pdf":
	default:
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedKYCExtensions[ext]
}

type submitKYCHTTPInput struct {
	BusinessName  string `json:"business_name"`
	TIN           string `json:"tin"`
	IDDocumentURL string `json:"id_document_url"`
}

func (h *Handler) SubmitKYC(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in submitKYCHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	sub, vErrs, err := h.service.SubmitKYC(r.Context(), userID, r.PathValue("orgID"), in.BusinessName, in.TIN, in.IDDocumentURL)
	if err != nil {
		h.orgError(w, err, "submit verification")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your submission.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": sub})
}

// ListKYCAttempts returns the submit/decide history for the Settings
// verification tab (any active member).
func (h *Handler) ListKYCAttempts(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	attempts, err := h.service.ListKYCAttempts(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "view verification history")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": attempts})
}

type notificationPrefsHTTPInput struct {
	PaymentUpdated    bool `json:"payment_updated"`
	PaymentRefunded   bool `json:"payment_refunded"`
	PaymentExpired    bool `json:"payment_expired"`
	WithdrawalUpdates bool `json:"withdrawal_updates"`
	KYCDecisions      bool `json:"kyc_decisions"`
}

func (h *Handler) GetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	prefs, err := h.service.GetNotificationPrefs(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "view notification settings")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": prefs})
}

func (h *Handler) UpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in notificationPrefsHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	prefs, err := h.service.UpdateNotificationPrefs(r.Context(), userID, r.PathValue("orgID"), NotificationPrefs{
		PaymentUpdated: in.PaymentUpdated, PaymentRefunded: in.PaymentRefunded,
		PaymentExpired: in.PaymentExpired, WithdrawalUpdates: in.WithdrawalUpdates,
		KYCDecisions: in.KYCDecisions,
	})
	if err != nil {
		h.orgError(w, err, "change notification settings")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": prefs})
}

func (h *Handler) GetKYCSubmission(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	sub, status, err := h.service.GetKYCSubmission(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		if errors.Is(err, ErrKYCNotSubmitted) {
			httputil.Error(w, http.StatusNotFound, "not_submitted", "No verification submission yet.", nil)
			return
		}
		h.orgError(w, err, "view verification")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"submission": sub, "kyc_status": status}})
}

func (h *Handler) UploadKYCDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.service.CheckOrgPermission(r.Context(), userID, orgID, PermManageOrg); err != nil {
		h.orgError(w, err, "upload verification documents")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKYCDocumentBytes+1<<20)
	if err := r.ParseMultipartForm(maxKYCDocumentBytes); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Document must be under 5MB.", nil)
		return
	}
	file, header, err := r.FormFile("document")
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Attach the ID document as the 'document' field.", nil)
		return
	}
	defer file.Close()
	if header.Size > maxKYCDocumentBytes {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Document must be under 5MB.", nil)
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store document.", err)
		return
	}
	detected := http.DetectContentType(head)
	if !kycContentTypeAllowed(detected, header.Filename) {
		httputil.Error(w, http.StatusUnprocessableEntity, "invalid_document", "Document must be a JPEG, PNG, WEBP image or PDF.", nil)
		return
	}
	if err := os.MkdirAll(kycUploadDir, 0o750); err != nil {
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store document.", err)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	name := orgID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ext
	path := filepath.Join(kycUploadDir, name)
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store document.", err)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store document.", err)
		return
	}
	_ = out.Close()
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id_document_url": filepath.ToSlash(path)}})
}

func (h *Handler) ServeKYCDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.service.CheckOrgPermission(r.Context(), userID, orgID, PermRead); err != nil {
		h.orgError(w, err, "view verification documents")
		return
	}
	sub, kycStatus, err := h.service.GetKYCSubmission(r.Context(), userID, orgID)
	if err != nil || sub.IDDocumentURL == "" {
		httputil.Error(w, http.StatusNotFound, "not_found", "No verification document.", nil)
		return
	}
	_ = kycStatus
	h.serveKYCDocumentPath(w, r, sub.IDDocumentURL)
}

func (h *Handler) serveKYCDocumentPath(w http.ResponseWriter, r *http.Request, docURL string) {
	clean := filepath.Clean(docURL)
	if strings.Contains(clean, "..") || !strings.HasPrefix(filepath.ToSlash(clean), "uploads/kyc/") {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Stored document path is invalid.", errors.New("kyc path escape"))
		return
	}
	http.ServeFile(w, r, clean)
}

// ---------- Admin: KYC review queue, decisions, live limits ----------
// All routes carry RequireAdmin; handlers use claimsIdentity (no org
// membership needed) and record the reviewer's email on decisions.

func (h *Handler) ListKYCQueue(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	items, err := h.service.ListKYCQueue(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		if errors.Is(err, ErrKYCQueueStatusUnknown) {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Status must be submitted, verified, rejected, pending, or all.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load review queue.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handler) ApproveKYC(w http.ResponseWriter, r *http.Request) {
	_, email, ok := claimsIdentity(w, r)
	if !ok {
		return
	}
	org, vErrs, err := h.service.ReviewKYC(r.Context(), email, r.PathValue("orgID"), true, "")
	if err != nil {
		h.reviewError(w, err, "approve verification")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your review.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

type rejectKYCHTTPInput struct {
	Reason string `json:"reason"`
}

func (h *Handler) RejectKYC(w http.ResponseWriter, r *http.Request) {
	_, email, ok := claimsIdentity(w, r)
	if !ok {
		return
	}
	var in rejectKYCHTTPInput
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	org, vErrs, err := h.service.ReviewKYC(r.Context(), email, r.PathValue("orgID"), false, in.Reason)
	if err != nil {
		h.reviewError(w, err, "reject verification")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "A rejection reason is required.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

func (h *Handler) reviewError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, ErrOrgNotFound):
		httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
	case errors.Is(err, ErrKYCNotInReview):
		httputil.Error(w, http.StatusConflict, "not_in_review", "Only organizations with a submission awaiting review can be decided.", nil)
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to "+action+".", err)
	}
}

type updateOrgLimitsHTTPInput struct {
	LiveMaxTxnAmount   string `json:"live_max_txn_amount"`
	LiveDailyVolumeCap string `json:"live_daily_volume_cap"`
}

func (h *Handler) UpdateOrgLiveLimits(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	var in updateOrgLimitsHTTPInput
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	org, vErrs, err := h.service.UpdateOrgLiveLimits(r.Context(), r.PathValue("orgID"), in.LiveMaxTxnAmount, in.LiveDailyVolumeCap)
	if err != nil {
		if errors.Is(err, ErrOrgNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to update limits.", err)
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Limits must be positive numbers, or empty for the platform default.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

// AdminServeKYCDocument streams an org's ID document without a membership
// check (reviewers are rarely members). Path confinement identical to the
// member route.
func (h *Handler) AdminServeKYCDocument(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	sub, err := h.service.AdminGetKYCSubmission(r.Context(), r.PathValue("orgID"))
	if err != nil {
		if errors.Is(err, ErrKYCNotSubmitted) {
			httputil.Error(w, http.StatusNotFound, "not_found", "No verification document.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load document.", err)
		return
	}
	if sub.IDDocumentURL == "" {
		httputil.Error(w, http.StatusNotFound, "not_found", "No verification document.", nil)
		return
	}
	h.serveKYCDocumentPath(w, r, sub.IDDocumentURL)
}