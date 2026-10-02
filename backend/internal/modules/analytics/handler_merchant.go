package analytics

import (
	"net/http"
	"strings"

	"lipago/internal/modules/orgs"
	"lipago/internal/modules/payments"
	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"
)

// SetOrgsService wires org membership resolution (merchant space). Roles
// come from the database via CheckOrgPermission — never from the client.
func (h *Handler) SetOrgsService(orgs *orgs.Service) {
	h.orgs = orgs
}

// orgMember resolves the caller's active membership for an org path value.
func (h *Handler) orgMember(w http.ResponseWriter, r *http.Request, orgID string, perm orgs.Permission, action string) (orgs.OrgMember, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return orgs.OrgMember{}, false
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization service is not configured.", nil)
		return orgs.OrgMember{}, false
	}
	member, err := h.orgs.CheckOrgPermission(r.Context(), claims.Subject, orgID, perm)
	if err != nil {
		httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this organization.", nil)
		return orgs.OrgMember{}, false
	}
	_ = action
	return member, true
}

// scoped pins Params to the caller's org (+ optional app_id validated to
// belong to that org) and reads the environment toggle (live default).
// Merchant track only — creator accounts use scopedCreator and their own
// /api/v1/creator/* analytics endpoints instead.
func (h *Handler) scoped(w http.ResponseWriter, r *http.Request, p Params) (Params, orgs.OrgMember, string, bool) {
	return h.scopedForKind(w, r, p, orgs.AccountKindMerchant)
}

// scopedCreator pins Params exactly like scoped but enforces the creator
// track — the gate for /api/v1/creator/orgs/* analytics endpoints.
func (h *Handler) scopedCreator(w http.ResponseWriter, r *http.Request, p Params) (Params, orgs.OrgMember, string, bool) {
	return h.scopedForKind(w, r, p, orgs.AccountKindCreator)
}

func (h *Handler) scopedForKind(w http.ResponseWriter, r *http.Request, p Params, kind string) (Params, orgs.OrgMember, string, bool) {
	orgID := strings.TrimSpace(r.PathValue("orgID"))
	member, ok := h.orgMember(w, r, orgID, orgs.PermRead, "view analytics")
	if !ok {
		return p, member, "", false
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization service is not configured.", nil)
		return p, member, "", false
	}
	org, err := h.orgs.RequireAccountKind(r.Context(), member.UserID, orgID, orgs.PermRead, kind)
	if err != nil {
		httputil.Error(w, http.StatusForbidden, "wrong_kind", "This analytics view belongs to the other workspace — use your matching dashboard instead.", nil)
		return p, member, "", false
	}
	_ = org
	env := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("environment")))
	if env == "" {
		env = "live"
	}
	if env != "live" && env != "sandbox" {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "environment must be live or sandbox.", nil)
		return p, member, "", false
	}
	p = scoped(p, orgID, "")
	if appID := strings.TrimSpace(r.URL.Query().Get("app_id")); appID != "" {
		appOrg, err := h.orgs.OrgIDForApp(r.Context(), appID)
		if err != nil || appOrg != orgID {
			httputil.Error(w, http.StatusForbidden, "forbidden", "That app does not belong to this organization.", nil)
			return p, member, "", false
		}
		p.AppID = appID
	}
	return p, member, env, true
}

func (h *Handler) MerchantOverview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, _, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	out, err := h.service.MerchantOverview(r.Context(), p, env)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load analytics.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) MerchantMethods(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, _, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	out, err := h.service.MerchantChannels(r.Context(), p, env)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load payment methods.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) MerchantPeakHours(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, _, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	out, err := h.service.MerchantPeakHours(r.Context(), p, env)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load peak hours.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) MerchantCustomers(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, member, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	// Developers see operational aggregates but no payer-level detail.
	showDetail := member.Role != orgs.RoleDeveloper
	out, err := h.service.MerchantCustomers(r.Context(), p, env, showDetail, payments.MaskPhone)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load customer analytics.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) MerchantFailures(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	// Org-scoped failure breakdown (no org_id query param accepted).
	p, _, _, ok = h.scoped(w, r, p)
	if !ok {
		return
	}
	out, err := h.service.FailureBreakdown(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load failures.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) MerchantApps(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, _, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	out, err := h.service.MerchantApps(r.Context(), p, env)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load app comparison.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}
