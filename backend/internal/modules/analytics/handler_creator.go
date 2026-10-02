package analytics

import (
	"net/http"

	"lipago/internal/modules/payments"
	"lipago/internal/shared/httputil"
)

// ---------- Creator analytics (Part 3 track split) ----------
//
// Distinct /api/v1/creator/orgs/* endpoints for personal creator
// workspaces: overview + supporters (customers), same rollup core as the
// merchant track. No methods/peak-hours/failures/apps/settlements surface
// — creators get the simplified view; merchants keep the full suite on
// /api/v1/orgs/*. Each handler enforces the creator track first.

func (h *Handler) CreatorOverview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, _, env, ok := h.scopedCreator(w, r, p)
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

func (h *Handler) CreatorSupporters(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, member, env, ok := h.scopedCreator(w, r, p)
	if !ok {
		return
	}
	// Creators are always their own sole owner: full supporter detail is
	// theirs to see (masked display helpers live in the frontend).
	_ = member
	out, err := h.service.MerchantCustomers(r.Context(), p, env, true, payments.MaskPhone)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load supporters.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}
