package payments

import (
	"errors"
	"net/http"
	"strings"

	"lipago/internal/modules/orgs"
	"lipago/internal/modules/payments/provider"
	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"
)

// ---------- Creator money plane (Part 3 track split) ----------
//
// Distinct /api/v1/creator/* endpoints for personal creator workspaces.
// They share the ledger, order, and withdrawal service core with the
// merchant track (same behavior regardless of kind) but each handler
// enforces the creator track first: merchant accounts get 403 wrong_kind,
// never silent success. No API keys, webhook endpoints, or delivery logs
// here — creators have no Developers surface; receiving runs through the
// support page's managed app.

// requireCreatorAppRole resolves the caller's role for the path app and
// enforces one permission plus the creator track — the mirror of
// requireOrgRole. Returns the caller user id for handlers that need it.
func (h *Handler) requireCreatorAppRole(w http.ResponseWriter, r *http.Request, appID string, perm orgs.Permission) (string, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return "", false
	}
	if h.orgs == nil {
		h.log().Error("org service not wired for creator route", "path", r.URL.Path)
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Creator access is not configured.", nil)
		return "", false
	}
	if _, err := h.orgs.RequireCreatorApp(r.Context(), claims.Subject, appID, perm); err != nil {
		if errors.Is(err, orgs.ErrNotOrgMember) || errors.Is(err, orgs.ErrOrgNotFound) {
			httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this app.", nil)
			return "", false
		}
		if errors.Is(err, orgs.ErrForbidden) {
			httputil.Error(w, http.StatusForbidden, "forbidden", "Your role doesn't allow this action.", nil)
			return "", false
		}
		if errors.Is(err, orgs.ErrNotCreatorOrg) {
			httputil.Error(w, http.StatusForbidden, "wrong_kind", "This action is only available to creator accounts — use your business workspace instead.", nil)
			return "", false
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to verify app access.", err)
		return "", false
	}
	return claims.Subject, true
}

// requireCreatorCaller enforces the creator track for caller-scoped (not
// app-scoped) endpoints using the caller's active org. Accounts with no
// org yet (still onboarding) fall through to the empty list below.
func (h *Handler) requireCreatorCaller(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return false
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Creator access is not configured.", nil)
		return false
	}
	if mine, err := h.orgs.ListMyOrganizations(r.Context(), claims.Subject); err == nil {
		for _, o := range mine {
			if o.Status != orgs.MemberStatusActive {
				continue
			}
			kind := strings.TrimSpace(o.AccountKind)
			if kind == "" {
				kind = orgs.AccountKindMerchant
			}
			if kind != orgs.AccountKindCreator {
				httputil.Error(w, http.StatusForbidden, "wrong_kind", "This action is only available to creator accounts — use your business workspace instead.", nil)
				return false
			}
			break
		}
	}
	return true
}

// CreatorListMyApps lists the creator's receiving app(s).
func (h *Handler) CreatorListMyApps(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	if !h.requireCreatorCaller(w, r) {
		return
	}

	apps, err := h.service.ListAppsForUser(r.Context(), claims.Subject)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list your apps.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": apps})
}

// CreatorGetAppBalance returns one receiving app's balance.
func (h *Handler) CreatorGetAppBalance(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermRead); !ok {
		return
	}

	balance, err := h.service.GetAppBalance(r.Context(), appID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load app balance.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": balance})
}

// CreatorListLedgerEntries lists one receiving app's ledger entries.
func (h *Handler) CreatorListLedgerEntries(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermRead); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListLedgerEntries(r.Context(), LedgerEntryListFilter{AppID: appID, Limit: limit, Offset: offset})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list ledger entries.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

// CreatorSearchOrders searches one receiving app's support orders. The app
// id is forced to the path value regardless of any client-supplied query
// param — same rule as the merchant track.
func (h *Handler) CreatorSearchOrders(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermRead); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.SearchPaymentOrders(r.Context(), PaymentOrderSearchFilter{
		AppID:  appID,
		Query:  r.URL.Query().Get("q"),
		Status: provider.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Phone:  r.URL.Query().Get("phone"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "search_failed", "Unable to search orders.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

// CreatorListWithdrawals lists one receiving app's payouts.
func (h *Handler) CreatorListWithdrawals(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermRead); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListWithdrawals(r.Context(), WithdrawalListFilter{AppID: appID, Limit: limit, Offset: offset})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list payouts.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

// CreatorCreateWithdrawal requests a payout to the saved, OTP-verified
// destination (enforced in the service — same core as the merchant track).
func (h *Handler) CreatorCreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	userID, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermWithdraw)
	if !ok {
		return
	}
	if !h.requireNotSuspended(w, r, appID) {
		return
	}
	h.createWithdrawalForApp(w, r, appID, userID, IdempotencyEndpointCreatorWithdrawalsCreate)
}

// CreatorApproveWithdrawal approves a requested payout (owner only —
// creators are single-member, so this is always the account holder).
func (h *Handler) CreatorApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	userID, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermWithdraw)
	if !ok {
		return
	}
	if !h.requireNotSuspended(w, r, appID) {
		return
	}

	withdrawal, err := h.service.ApproveWithdrawal(r.Context(), r.PathValue("withdrawalID"), userID)
	if err != nil {
		h.withdrawalTransitionError(w, err, "approved")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

// CreatorRejectWithdrawal rejects a requested payout.
func (h *Handler) CreatorRejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireCreatorAppRole(w, r, appID, orgs.PermWithdraw); !ok {
		return
	}

	withdrawal, err := h.service.RejectWithdrawal(r.Context(), r.PathValue("withdrawalID"))
	if err != nil {
		h.withdrawalTransitionError(w, err, "rejected")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

// CreatorOrgLimitsUsage serves the creator Settings payout view: effective
// caps and today's volume. Same caps core as the merchant track,
// creator-gated.
func (h *Handler) CreatorOrgLimitsUsage(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.orgs.RequireCreatorOrg(r.Context(), claims.Subject, orgID, orgs.PermRead); err != nil {
		if errors.Is(err, orgs.ErrNotOrgMember) {
			httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this organization.", nil)
			return
		}
		if errors.Is(err, orgs.ErrForbidden) {
			httputil.Error(w, http.StatusForbidden, "forbidden", "Your role doesn't allow this action.", nil)
			return
		}
		if errors.Is(err, orgs.ErrNotCreatorOrg) {
			httputil.Error(w, http.StatusForbidden, "wrong_kind", "This action is only available to creator accounts — use your business workspace instead.", nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to verify organization access.", err)
		return
	}
	usage, err := h.service.OrgLimitsUsage(r.Context(), orgID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load limits usage.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": usage})
}

// Individual* are the canonical internal names used by the new individual
// API aliases. The Creator* methods remain for deprecated route support.
func (h *Handler) IndividualListMyApps(w http.ResponseWriter, r *http.Request) {
	h.CreatorListMyApps(w, r)
}

func (h *Handler) IndividualGetAppBalance(w http.ResponseWriter, r *http.Request) {
	h.CreatorGetAppBalance(w, r)
}

func (h *Handler) IndividualListLedgerEntries(w http.ResponseWriter, r *http.Request) {
	h.CreatorListLedgerEntries(w, r)
}

func (h *Handler) IndividualSearchOrders(w http.ResponseWriter, r *http.Request) {
	h.CreatorSearchOrders(w, r)
}

func (h *Handler) IndividualListWithdrawals(w http.ResponseWriter, r *http.Request) {
	h.CreatorListWithdrawals(w, r)
}

func (h *Handler) IndividualCreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.CreatorCreateWithdrawal(w, r)
}

func (h *Handler) IndividualApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.CreatorApproveWithdrawal(w, r)
}

func (h *Handler) IndividualRejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.CreatorRejectWithdrawal(w, r)
}

func (h *Handler) IndividualAccountLimitsUsage(w http.ResponseWriter, r *http.Request) {
	h.CreatorOrgLimitsUsage(w, r)
}
