package analytics

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"
)

// Handler serves /api/v1/admin/analytics/... (admin space only). Every
// handler parses shared Params (400 on bad input) and returns {data}.
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
		h.logger.Error("analytics request failed", "code", code, "status", status, "error", err)
	}
	httputil.Error(w, status, code, message, nil)
}

func (h *Handler) params(w http.ResponseWriter, r *http.Request) (Params, bool) {
	p, err := ParseParams(r, true)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return Params{}, false
	}
	return p, true
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.Overview(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load overview.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) Providers(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.ProviderStats(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load provider stats.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) TopMerchants(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	switch sortBy {
	case "", "tx_count", "tpv", "revenue", "success_rate":
		if sortBy == "" {
			sortBy = "tx_count"
		}
	default:
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "sort must be tx_count, tpv, revenue, or success_rate.", nil)
		return
	}
	out, err := h.service.TopMerchants(r.Context(), p, sortBy)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load top merchants.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) SignupFunnel(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.SignupFunnel(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load signup funnel.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) DormantMerchants(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.DormantMerchants(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load dormant merchants.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) ChurnRiskMerchants(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.ChurnRiskMerchants(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load churn-risk merchants.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) Failures(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
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

func (h *Handler) Withdrawals(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.WithdrawalStats(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load withdrawal stats.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) Webhooks(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.WebhookHealth(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load webhook health.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) StuckOrders(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.StuckOrders(r.Context(), p)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load stuck orders.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) Unreconciled(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	out, err := h.service.Unreconciled(r.Context(), p, 200)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load unreconciled items.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) NegativeBalances(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.NegativeBalances(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load negative balances.", err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": out})
}

// Export serves GET /api/v1/admin/analytics/export/{report}?format=csv.
// Reads are not audited; exports are (they can carry merchant data).
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	report := strings.TrimSpace(r.PathValue("report"))
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Only format=csv is supported.", nil)
		return
	}
	filename, data, err := h.service.ExportCSV(r.Context(), report, p)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unknown report") {
			httputil.Error(w, http.StatusNotFound, "not_found", err.Error(), nil)
			return
		}
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to build export.", err)
		return
	}
	claims, _ := middleware.ClaimsFromContext(r.Context())
	if auditErr := h.service.LogExport(r.Context(), claims.Subject, claims.Email, report, clientIP(r)); auditErr != nil {
		h.logger.Error("analytics export audit failed", "report", report, "error", auditErr)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func clientIP(r *http.Request) string {
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
