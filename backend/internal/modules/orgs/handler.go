package orgs

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"azsubay-payments-gateway/internal/platform/middleware"
	"azsubay-payments-gateway/internal/shared/httputil"
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
	org, vErrs, err := h.service.UpdateOrganization(r.Context(), userID, r.PathValue("orgID"), in.Name, in.BusinessName)
	if err != nil {
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
