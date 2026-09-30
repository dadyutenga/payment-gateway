package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lipago/internal/modules/orgs"
	"lipago/internal/platform/auth"
	"lipago/internal/shared/httputil"
)

// ---------- Creator payout destinations (Part 5 risk posture) ----------
//
// One saved mobile-money destination per creator org, tied to the
// verified identity: OTP-verified at save time, 24h cooling on changes,
// withdrawals must target the effective destination. Merchant orgs keep
// per-request destinations with no saved store (unchanged).

// OTPVerifier is the narrow auth surface destination saves need. The
// concrete *auth.Service satisfies it; tests use a fake.
type OTPVerifier interface {
	VerifyOTP(ctx context.Context, userID, channel, purpose, code string) error
}

// SetAuthService wires OTP verification for destination saves. Without
// it, saves fail closed rather than skipping the check.
func (h *Handler) SetAuthService(service OTPVerifier) {
	h.auth = service
}

// withdrawalDestinationError maps the saved-destination enforcement
// errors for both withdrawal creation paths (merchant + admin).
func (h *Handler) withdrawalDestinationError(state *idempotencyState, w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, ErrWithdrawalDestinationMismatch):
		h.writeError(state, w, r, http.StatusUnprocessableEntity, "destination_mismatch", "Withdrawal destination does not match the verified payout destination.", nil)
		return true
	case errors.Is(err, ErrWithdrawalDestinationCooling):
		h.writeError(state, w, r, http.StatusConflict, "destination_cooling", "Payout destination change is still in its 24h cooling period.", nil)
		return true
	}
	return false
}

// GetPayoutDestination returns the creator's saved payout destination
// (any active member may read). Null data when none was ever saved.
func (h *Handler) GetPayoutDestination(w http.ResponseWriter, r *http.Request) {
	userID, ok := merchantClaimsUserID(w, r)
	if !ok {
		return
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization access is not configured.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.orgs.RequireCreatorOrg(r.Context(), userID, orgID, orgs.PermRead); err != nil {
		h.supportOrgError(w, err, "view the payout destination")
		return
	}
	dest, found, err := h.service.GetCreatorPayoutDestination(r.Context(), orgID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load the payout destination.", err)
		return
	}
	if !found {
		httputil.JSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": dest})
}

type savePayoutDestinationHTTPInput struct {
	Provider   string `json:"provider"`
	Phone      string `json:"phone"`
	AccountName string `json:"account_name"`
	OTPChannel string `json:"otp_channel"`
	OTPCode    string `json:"otp_code"`
}

// SavePayoutDestination creates or changes the creator's payout
// destination. Requires a fresh OTP (purpose payout_destination,
// requested via the standard OTP endpoint) plus a verified individual
// identity whose name the attested account name must match. Changes take
// effect after the 24h cooling period.
func (h *Handler) SavePayoutDestination(w http.ResponseWriter, r *http.Request) {
	userID, ok := merchantClaimsUserID(w, r)
	if !ok {
		return
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization access is not configured.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.orgs.RequireCreatorOrg(r.Context(), userID, orgID, orgs.PermWithdraw); err != nil {
		h.supportOrgError(w, err, "manage the payout destination")
		return
	}
	var in savePayoutDestinationHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	if h.auth == nil {
		h.fail(w, http.StatusServiceUnavailable, "otp_not_configured", "Verification sending is not configured on this deployment.", errors.New("auth service not wired"))
		return
	}
	if err := h.auth.VerifyOTP(r.Context(), userID, in.OTPChannel, auth.OTPPurposePayoutDestination, in.OTPCode); err != nil {
		h.otpVerifyError(w, err)
		return
	}
	sub, status, err := h.orgs.GetKYCSubmission(r.Context(), userID, orgID)
	if err != nil {
		if errors.Is(err, orgs.ErrKYCNotSubmitted) {
			httputil.Error(w, http.StatusConflict, "identity_unverified", "Verify your identity before saving a payout destination.", nil)
			return
		}
		h.supportOrgError(w, err, "check verification")
		return
	}
	if status != "verified" || strings.TrimSpace(sub.FullName) == "" {
		httputil.Error(w, http.StatusConflict, "identity_unverified", "Verify your identity before saving a payout destination.", nil)
		return
	}
	dest, vErrs, err := h.service.SaveCreatorPayoutDestination(r.Context(), orgID, sub.FullName, SavePayoutDestinationInput{
		Provider: in.Provider, Phone: in.Phone, AccountName: in.AccountName,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrCreatorIdentityUnverified):
			httputil.Error(w, http.StatusConflict, "identity_unverified", "Verify your identity before saving a payout destination.", nil)
		default:
			h.supportOrgError(w, err, "save the payout destination")
		}
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your payout destination.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": dest})
}

// otpVerifyError mirrors the auth OTP verify mapping so destination saves
// speak the same error codes as signup verification.
func (h *Handler) otpVerifyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrOTPNoActiveCode):
		httputil.Error(w, http.StatusUnprocessableEntity, "no_active_code", "No active code — request a fresh one.", nil)
	case errors.Is(err, auth.ErrOTPExpired):
		httputil.Error(w, http.StatusUnprocessableEntity, "code_expired", "Code expired — request a fresh one.", nil)
	case errors.Is(err, auth.ErrOTPLocked):
		httputil.Error(w, http.StatusUnprocessableEntity, "code_locked", "Too many wrong attempts — request a fresh code.", nil)
	case errors.Is(err, auth.ErrOTPInvalid):
		httputil.Error(w, http.StatusUnprocessableEntity, "invalid_code", "Invalid code.", nil)
	default:
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to verify code.", nil)
	}
}
