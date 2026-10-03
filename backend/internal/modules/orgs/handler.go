package orgs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"lipago/internal/platform/middleware"
	"lipago/internal/platform/storage"
	"lipago/internal/shared/httputil"

	"github.com/google/uuid"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
	storage storage.Store
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

func (h *Handler) SetStorage(store storage.Store) {
	h.storage = store
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
	case errors.Is(err, ErrSingleOrg):
		httputil.Error(w, http.StatusConflict, "single_org", "Each account belongs to a single organization.", nil)
	case errors.Is(err, ErrHandleTaken):
		httputil.Error(w, http.StatusConflict, "handle_taken", "That handle is already taken — try another.", nil)
	case errors.Is(err, ErrHandleInvalid):
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Handle must be 3-30 lowercase letters, numbers, dots, hyphens or underscores.", nil)
	case errors.Is(err, ErrSurveyNotFound):
		httputil.Error(w, http.StatusNotFound, "not_submitted", "No onboarding survey yet.", nil)
	case errors.Is(err, ErrKindSwitchSubmitted):
		httputil.Error(w, http.StatusConflict, "kind_switch_locked", "Account kind can only be switched before verification is submitted — contact support.", nil)
	case errors.Is(err, ErrAccountKindImmutable):
		httputil.Error(w, http.StatusConflict, "kind_immutable", "Only creator accounts can switch to the merchant track.", nil)
	case errors.Is(err, ErrKindSwitchDisabled):
		httputil.Error(w, http.StatusGone, "kind_immutable", "Account kind cannot be switched — business and creator accounts are fully separate. Contact support if you signed up on the wrong track.", nil)
	case errors.Is(err, ErrNotCreatorOrg):
		httputil.Error(w, http.StatusForbidden, "wrong_kind", "This action is only available to creator accounts.", nil)
	case errors.Is(err, ErrNotMerchantOrg):
		httputil.Error(w, http.StatusForbidden, "wrong_kind", "Team management is only available to business accounts — creator accounts are personal.", nil)
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to complete the organization request.", err)
	}
}

type createOrgHTTPInput struct {
	Name         string `json:"name"`
	BusinessName string `json:"business_name"`
	AccountKind  string `json:"account_kind"`
	DisplayName  string `json:"display_name"`
	Handle       string `json:"handle"`
	Bio          string `json:"bio"`
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
	if strings.TrimSpace(in.AccountKind) != "" {
		httputil.Error(w, http.StatusGone, "track_endpoint_required", "Use the merchant or creator account endpoint; account_kind is not client-selectable.", nil)
		return
	}
	created, vErrs, err := h.service.CreateOrganization(r.Context(), userID, in.Name, in.BusinessName)
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
	member, err := h.service.CheckOrgPermission(r.Context(), userID, created.ID, PermRead)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": OrganizationWithRole{Organization: created, Role: member.Role, Status: member.Status}})
}

type createMerchantOrgHTTPInput struct {
	Name         string `json:"name"`
	BusinessName string `json:"business_name"`
}

// CreateMerchantOrganization creates a business-track org. The kind comes
// from the endpoint called, never from a client-supplied field, so a
// client cannot flip kind post-hoc via a shared payload.
func (h *Handler) CreateMerchantOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in createMerchantOrgHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	created, vErrs, err := h.service.CreateOrganization(r.Context(), userID, in.Name, in.BusinessName)
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your organization input.", vErrs)
		return
	}
	if err != nil {
		if errors.Is(err, ErrSingleOrg) {
			httputil.Error(w, http.StatusConflict, "single_org", "Each account belongs to a single organization.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	member, err := h.service.CheckOrgPermission(r.Context(), userID, created.ID, PermRead)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": OrganizationWithRole{Organization: created, Role: member.Role, Status: member.Status}})
}

type createCreatorOrgHTTPInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Handle      string `json:"handle"`
	Bio         string `json:"bio"`
}

// CreateCreatorOrganization creates a personal creator-track account. The
// kind comes from the endpoint called, never from a client-supplied field.
func (h *Handler) CreateCreatorOrganization(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in createCreatorOrgHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	created, vErrs, err := h.service.CreateCreatorOrganization(r.Context(), userID, in.Name, CreatorOrgInput{
		DisplayName: in.DisplayName, Handle: in.Handle, Bio: in.Bio,
	})
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your organization input.", vErrs)
		return
	}
	if err != nil {
		if errors.Is(err, ErrSingleOrg) {
			httputil.Error(w, http.StatusConflict, "single_org", "Each account belongs to a single organization.", nil)
			return
		}
		if errors.Is(err, ErrHandleTaken) {
			httputil.Error(w, http.StatusConflict, "handle_taken", "That handle is already taken — try another.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	member, err := h.service.CheckOrgPermission(r.Context(), userID, created.ID, PermRead)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create organization.", err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": OrganizationWithRole{Organization: created, Role: member.Role, Status: member.Status}})
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
	DisplayName  string `json:"display_name"`
	Handle       string `json:"handle"`
	Bio          string `json:"bio"`
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
		DisplayName: in.DisplayName, Handle: in.Handle, Bio: in.Bio,
	})
	if err != nil {
		if errors.Is(err, ErrReverificationRequired) {
			httputil.Error(w, http.StatusConflict, "reverification_required", "Business name and TIN are locked after verification — resubmit verification to change them.", nil)
			return
		}
		if errors.Is(err, ErrHandleTaken) {
			httputil.Error(w, http.StatusConflict, "handle_taken", "That handle is already taken — try another.", nil)
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

// maxKYCDocumentBytes caps ID and selfie uploads at 5MB.
const maxKYCDocumentBytes = 5 << 20

const r2ObjectPrefix = "r2://"

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

type submitCreatorKYCHTTPInput struct {
	FullName          string `json:"full_name"`
	IDType            string `json:"id_type"`
	IDNumber          string `json:"id_number"`
	Dob               string `json:"dob"`
	IDDocumentURL     string `json:"id_document_url"`
	IDDocumentBackURL string `json:"id_document_back_url"`
	SelfieURL         string `json:"selfie_url"`
}

// SubmitCreatorKYC files individual verification for creator accounts
// (full name + DOB + national ID + front document + optional back side +
// v1 selfie). Merchant accounts keep using the business endpoint above.
func (h *Handler) SubmitCreatorKYC(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in submitCreatorKYCHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	sub, vErrs, err := h.service.SubmitCreatorKYC(r.Context(), userID, r.PathValue("orgID"), CreatorKYCInput{
		FullName: in.FullName, IDType: in.IDType, IDNumber: in.IDNumber, Dob: in.Dob,
		DocURL: in.IDDocumentURL, DocBackURL: in.IDDocumentBackURL, SelfieURL: in.SelfieURL,
	})
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

type creatorSurveyHTTPInput struct {
	DisplayName        string   `json:"display_name"`
	Category           string   `json:"category"`
	CategoryOther      string   `json:"category_other"`
	ReferralSource     string   `json:"referral_source"`
	UseCases           []string `json:"use_cases"`
	ExpectedVolumeBand string   `json:"expected_volume_band"`
	ExpectedTxnBand    string   `json:"expected_txn_band"`
}

// SaveCreatorSurvey stores (or replaces) a creator org's onboarding
// answers (owner/manage_org). Creator accounts only.
func (h *Handler) SaveCreatorSurvey(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	var in creatorSurveyHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	survey, vErrs, err := h.service.SaveCreatorSurvey(r.Context(), userID, r.PathValue("orgID"), CreatorSurveyInput{
		DisplayName: in.DisplayName, Category: in.Category, CategoryOther: in.CategoryOther,
		ReferralSource: in.ReferralSource, UseCases: in.UseCases,
		ExpectedVolumeBand: in.ExpectedVolumeBand, ExpectedTxnBand: in.ExpectedTxnBand,
	})
	if err != nil {
		h.orgError(w, err, "save the onboarding survey")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your answers.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": survey})
}

// GetCreatorSurvey returns a creator org's onboarding answers plus the
// derived starting risk tier (any active member may read).
func (h *Handler) GetCreatorSurvey(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	if _, err := h.service.RequireCreatorOrg(r.Context(), userID, r.PathValue("orgID"), PermRead); err != nil {
		h.orgError(w, err, "view the individual onboarding survey")
		return
	}
	survey, err := h.service.GetCreatorSurvey(r.Context(), userID, r.PathValue("orgID"))
	if err != nil {
		h.orgError(w, err, "view the onboarding survey")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": survey})
}

// SwitchCreatorToMerchant is disabled: business and creator accounts are
// fully separate tracks with no self-service conversion (each has its own
// KYC). The route stays registered for one deploy cycle so stale clients
// get a clear 410 instead of a bare 404; kind changes, if ever needed,
// are support-assisted directly in the database.
func (h *Handler) SwitchCreatorToMerchant(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsUserID(w, r); !ok {
		return
	}
	h.orgError(w, ErrKindSwitchDisabled, "switch account kind")
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
	path, stored := h.storeKYCUpload(w, r, orgID, "document", false, "")
	if !stored {
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id_document_url": path}})
}

var allowedSelfieExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

// selfieContentTypeAllowed sniffs raster images only (v1 selfie is a
// simple photo — PDF makes no sense and SVG is a stored-XSS vector).
func selfieContentTypeAllowed(detected, filename string) bool {
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedSelfieExtensions[ext]
}

// UploadKYCSelfie stores the creator's v1 selfie photo (owner/manage_org)
// and returns its private location for the KYC submit call. True liveness
// detection is future scope — this endpoint intentionally accepts a plain
// photo and says so in the UI copy.
func (h *Handler) UploadKYCSelfie(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.service.RequireCreatorOrg(r.Context(), userID, orgID, PermManageOrg); err != nil {
		h.orgError(w, err, "upload the selfie photo")
		return
	}
	path, stored := h.storeKYCUpload(w, r, orgID, "selfie", true, "-selfie")
	if !stored {
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"selfie_url": path}})
}

// storeKYCUpload parses one multipart file and stores it under
// uploads/kyc/. imagesOnly restricts to raster photos (selfies); the
// document path additionally allows PDFs. It writes the error response
// itself and reports ok=false when it does.
func (h *Handler) storeKYCUpload(w http.ResponseWriter, r *http.Request, orgID, formField string, imagesOnly bool, nameInfix string) (string, bool) {
	if h.storage == nil {
		h.fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured.", nil)
		return "", false
	}
	limit := int64(maxKYCDocumentBytes)
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(limit); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "File must be under 5MB.", nil)
		return "", false
	}
	file, header, err := r.FormFile(formField)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Attach the file as the '"+formField+"' field.", nil)
		return "", false
	}
	defer file.Close()
	if header.Size > limit {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "File must be under 5MB.", nil)
		return "", false
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store file.", err)
		return "", false
	}
	detected := http.DetectContentType(head)
	if imagesOnly {
		if !selfieContentTypeAllowed(detected, header.Filename) {
			httputil.Error(w, http.StatusUnprocessableEntity, "invalid_selfie", "Selfie must be a JPEG, PNG, or WEBP photo.", nil)
			return "", false
		}
	} else if !kycContentTypeAllowed(detected, header.Filename) {
		httputil.Error(w, http.StatusUnprocessableEntity, "invalid_document", "Document must be a JPEG, PNG, WEBP image or PDF.", nil)
		return "", false
	}
	if err := storage.ValidateUpload(detected, header.Filename, header.Size, limit, allowedKYCExtensions); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid_document", "Document type or size is not allowed.", err)
		return "", false
	}
	objectID := uuid.NewString() + strings.TrimSpace(nameInfix)
	key, err := storage.BuildKey("private/kyc/"+orgID, objectID, header.Filename, allowedKYCExtensions)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid_document", "Unable to name the uploaded document.", err)
		return "", false
	}
	if _, err := h.storage.Put(r.Context(), key, file, detected, header.Size); err != nil {
		h.fail(w, http.StatusBadGateway, "upload_failed", "Unable to store file.", err)
		return "", false
	}
	return r2ObjectPrefix + key, true
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
	h.auditKYCView(r, orgID, "document")
	h.serveKYCDocumentPath(w, r, sub.IDDocumentURL)
}

func (h *Handler) serveKYCDocumentPath(w http.ResponseWriter, r *http.Request, docURL string) {
	if strings.HasPrefix(docURL, r2ObjectPrefix) {
		if h.storage == nil {
			h.fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured.", nil)
			return
		}
		object, err := h.storage.Get(r.Context(), strings.TrimPrefix(docURL, r2ObjectPrefix))
		if err != nil {
			h.fail(w, http.StatusBadGateway, "download_failed", "Unable to load document.", err)
			return
		}
		defer object.Body.Close()
		if object.Info.ContentType != "" {
			w.Header().Set("Content-Type", object.Info.ContentType)
		}
		if object.Info.Size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(object.Info.Size, 10))
		}
		_, _ = io.Copy(w, object.Body)
		return
	}
	// Legacy local objects remain readable while cmd/migrate-uploads moves
	// existing rows into R2. New uploads never use this branch.
	clean := filepath.Clean(docURL)
	if strings.Contains(clean, "..") || !strings.HasPrefix(filepath.ToSlash(clean), "uploads/kyc/") {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Stored document path is invalid.", errors.New("kyc path escape"))
		return
	}
	http.ServeFile(w, r, clean)
}

// maxLogoBytes caps branding logo uploads at 2MB.
const maxLogoBytes = 2 << 20

var allowedLogoExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

// logoContentTypeAllowed sniffs raster images only. SVG is deliberately
// excluded (stored-XSS vector when served); PDF makes no sense for a logo.
func logoContentTypeAllowed(detected, filename string) bool {
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedLogoExtensions[ext]
}

// UploadOrgLogo stores a branding logo (owner/manage_org) and records its
// private path on the org. External URLs remain settable via PATCH.
func (h *Handler) UploadOrgLogo(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.service.CheckOrgPermission(r.Context(), userID, orgID, PermManageOrg); err != nil {
		h.orgError(w, err, "upload the logo")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes+1<<20)
	if err := r.ParseMultipartForm(maxLogoBytes); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Logo must be under 2MB.", nil)
		return
	}
	file, header, err := r.FormFile("logo")
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Attach the logo as the 'logo' field.", nil)
		return
	}
	defer file.Close()
	if header.Size > maxLogoBytes {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Logo must be under 2MB.", nil)
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.fail(w, http.StatusInternalServerError, "upload_failed", "Unable to store logo.", err)
		return
	}
	detected := http.DetectContentType(head)
	if !logoContentTypeAllowed(detected, header.Filename) {
		httputil.Error(w, http.StatusUnprocessableEntity, "invalid_logo", "Logo must be a JPEG, PNG, or WEBP image.", nil)
		return
	}
	if h.storage == nil {
		h.fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured.", nil)
		return
	}
	if err := storage.ValidateUpload(detected, header.Filename, header.Size, maxLogoBytes, allowedLogoExtensions); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid_logo", "Logo type or size is not allowed.", err)
		return
	}
	key, err := storage.BuildKey("public/branding", orgID, header.Filename, allowedLogoExtensions)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid_logo", "Unable to name the uploaded logo.", err)
		return
	}
	if _, err := h.storage.Put(r.Context(), key, file, detected, header.Size); err != nil {
		h.fail(w, http.StatusBadGateway, "upload_failed", "Unable to store logo.", err)
		return
	}
	org, err := h.service.SetOrgLogoURL(r.Context(), userID, orgID, r2ObjectPrefix+key)
	if err != nil {
		_ = h.storage.Delete(r.Context(), key)
		h.orgError(w, err, "save the logo")
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": org})
}

// ServeOrgLogo streams the org's logo to active members. Uploaded files
// serve from disk (path-confined); external URLs redirect; anything else
// is 404 — never a public directory listing.
func (h *Handler) ServeOrgLogo(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	member, err := h.service.CheckOrgPermission(r.Context(), userID, orgID, PermRead)
	if err != nil {
		h.orgError(w, err, "view the logo")
		return
	}
	_ = member
	org, err := h.service.GetOrganization(r.Context(), userID, orgID)
	if err != nil {
		h.orgError(w, err, "view the logo")
		return
	}
	loc := strings.TrimSpace(org.LogoURL)
	if loc == "" {
		httputil.Error(w, http.StatusNotFound, "not_found", "No logo set.", nil)
		return
	}
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		http.Redirect(w, r, loc, http.StatusFound)
		return
	}
	if strings.HasPrefix(loc, r2ObjectPrefix) {
		if h.storage == nil {
			h.fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured.", nil)
			return
		}
		object, err := h.storage.Get(r.Context(), strings.TrimPrefix(loc, r2ObjectPrefix))
		if err != nil {
			h.fail(w, http.StatusBadGateway, "download_failed", "Unable to load the logo.", err)
			return
		}
		defer object.Body.Close()
		if object.Info.ContentType != "" {
			w.Header().Set("Content-Type", object.Info.ContentType)
		}
		_, _ = io.Copy(w, object.Body)
		return
	}
	clean := filepath.Clean(loc)
	if strings.Contains(clean, "..") || !strings.HasPrefix(filepath.ToSlash(clean), "uploads/branding/") {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Stored logo path is invalid.", errors.New("logo path escape"))
		return
	}
	http.ServeFile(w, r, clean)
}

// auditAdmin records an admin mutation best-effort (never fails the
// request). Callers are RequireAdminAuth-gated; identity comes from the
// verified claims, never client input.
func (h *Handler) auditAdmin(r *http.Request, action, targetType, targetID string, before, after map[string]any) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return
	}
	ip := requestIP(r)
	if err := h.service.WriteAudit(r.Context(), claims.Subject, claims.Email, action, targetType, targetID, ip, before, after); err != nil {
		h.logger.Warn("audit write failed", "action", action, "target", targetID, "error", err)
	}
}

func (h *Handler) auditKYCView(r *http.Request, orgID, kind string) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return
	}
	if err := h.service.WriteAudit(r.Context(), claims.Subject, claims.Email, "kyc."+kind+".viewed", "organization", orgID, requestIP(r), nil, map[string]any{"kind": kind}); err != nil {
		h.logger.Warn("KYC access audit failed", "org_id", orgID, "kind", kind, "error", err)
	}
}

func requestIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// All routes carry RequireAdmin; handlers use claimsIdentity (no org
// membership needed) and record the reviewer's email on decisions.

// PlatformStats serves GET /api/v1/admin/stats — the admin home
// dashboard snapshot. Route-gated by RequireAdminAuth.
func (h *Handler) PlatformStats(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	stats, err := h.service.PlatformStats(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load platform stats.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": stats})
}

// AdminOrgDetail serves GET /api/v1/admin/orgs/:id — the operator
// single-org view linked from analytics rows.
func (h *Handler) AdminOrgDetail(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	detail, err := h.service.AdminOrgDetail(r.Context(), r.PathValue("orgID"))
	if err != nil {
		if errors.Is(err, ErrOrgNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load organization.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": detail})
}

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
	h.auditAdmin(r, "kyc.approve", "organization", org.ID,
		map[string]any{"kyc_status": "submitted"}, map[string]any{"kyc_status": "verified"})
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
	h.auditAdmin(r, "kyc.reject", "organization", org.ID,
		map[string]any{"kyc_status": "submitted"},
		map[string]any{"kyc_status": "rejected", "reason": in.Reason})
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
	h.auditAdmin(r, "org.update_limits", "organization", org.ID, nil, map[string]any{
		"live_max_txn_amount": in.LiveMaxTxnAmount, "live_daily_volume_cap": in.LiveDailyVolumeCap,
	})
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

type suspendOrgHTTPInput struct {
	Reason string `json:"reason"`
}

// SuspendOrg freezes an org's live money movement (reason required).
func (h *Handler) SuspendOrg(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	var in suspendOrgHTTPInput
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	org, vErrs, err := h.service.SuspendOrg(r.Context(), r.PathValue("orgID"), in.Reason)
	if err != nil {
		if errors.Is(err, ErrOrgNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to suspend organization.", err)
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "A suspension reason is required.", vErrs)
		return
	}
	h.auditAdmin(r, "org.suspend", "organization", org.ID,
		map[string]any{"suspended": false},
		map[string]any{"suspended": true, "reason": in.Reason})
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

// UnsuspendOrg lifts an org suspension.
func (h *Handler) UnsuspendOrg(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	org, err := h.service.UnsuspendOrg(r.Context(), r.PathValue("orgID"))
	if err != nil {
		if errors.Is(err, ErrOrgNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to unsuspend organization.", err)
		return
	}
	h.auditAdmin(r, "org.unsuspend", "organization", org.ID,
		map[string]any{"suspended": true}, map[string]any{"suspended": false})
	httputil.JSON(w, http.StatusOK, map[string]any{"data": org})
}

// ListAudit serves the admin audit trail viewer (paginated, filterable).
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	q := r.URL.Query()
	limit, offset := 50, 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}
	entries, err := h.service.ListAudit(r.Context(),
		strings.TrimSpace(q.Get("action")), strings.TrimSpace(q.Get("actor")), limit, offset)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load audit log.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": entries})
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

// ServeKYCSelfie streams the creator's v1 selfie to active members of
// creator accounts only — merchants get 403, never an empty photo.
func (h *Handler) ServeKYCSelfie(w http.ResponseWriter, r *http.Request) {
	userID, ok := claimsUserID(w, r)
	if !ok {
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.service.RequireCreatorOrg(r.Context(), userID, orgID, PermRead); err != nil {
		h.orgError(w, err, "view the selfie photo")
		return
	}
	sub, kycStatus, err := h.service.GetKYCSubmission(r.Context(), userID, orgID)
	if err != nil || sub.SelfieURL == "" {
		httputil.Error(w, http.StatusNotFound, "not_found", "No selfie photo.", nil)
		return
	}
	_ = kycStatus
	h.auditKYCView(r, orgID, "selfie")
	h.serveKYCDocumentPath(w, r, sub.SelfieURL)
}

// AdminServeKYCSelfie streams an org's selfie without a membership check
// (reviewers are rarely members). Path confinement identical to the
// member route.
func (h *Handler) AdminServeKYCSelfie(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := claimsIdentity(w, r); !ok {
		return
	}
	sub, err := h.service.AdminGetKYCSubmission(r.Context(), r.PathValue("orgID"))
	if err != nil {
		if errors.Is(err, ErrKYCNotSubmitted) {
			httputil.Error(w, http.StatusNotFound, "not_found", "No selfie photo.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load selfie.", err)
		return
	}
	if sub.SelfieURL == "" {
		httputil.Error(w, http.StatusNotFound, "not_found", "No selfie photo.", nil)
		return
	}
	h.auditKYCView(r, r.PathValue("orgID"), "selfie")
	h.serveKYCDocumentPath(w, r, sub.SelfieURL)
}
