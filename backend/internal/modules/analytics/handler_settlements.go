package analytics

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"lipago/internal/modules/orgs"
	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"

	"github.com/go-pdf/fpdf"
)

// exportLimiter caps settlement exports per user (10/min). Analytics list
// reads are cheap; file generation is not.
type exportLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

var settlementExports = &exportLimiter{hits: map[string][]time.Time{}}

func (l *exportLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 10 {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// MerchantSettlements serves GET /orgs/:orgId/settlements?from&to&app_id&
// currency&format=json|csv|pdf. Money comes from the LEDGER (never
// rollups). Exports (csv/pdf) require owner/finance and are audited +
// rate-limited; reads need any active member.
func (h *Handler) MerchantSettlements(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(w, r)
	if !ok {
		return
	}
	p, member, env, ok := h.scoped(w, r, p)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	currency := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("currency")))
	entryLimit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err == nil && n > 0 && n <= 5000 {
			entryLimit = n
		}
	}
	if format == "pdf" && currency == "" {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "PDF statements need ?currency=XXX (one statement per currency).", nil)
		return
	}
	statement, err := h.service.MerchantSettlements(r.Context(), p, env, currency, entryLimit)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to build settlement.", err)
		return
	}
	if format == "json" {
		httputil.JSON(w, http.StatusOK, map[string]any{"data": statement})
		return
	}
	// Exports: owner/finance only, rate-limited, audited.
	if !orgs.Can(member.Role, orgs.PermWithdraw) {
		httputil.Error(w, http.StatusForbidden, "forbidden", "Only owners and finance members can export statements.", nil)
		return
	}
	claims, _ := middleware.ClaimsFromContext(r.Context())
	if !settlementExports.allow("settlement:" + claims.Subject) {
		httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many exports — try again in a minute.", nil)
		return
	}
	switch format {
	case "csv":
		data, err := settlementCSV(statement)
		if err != nil {
			h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to build CSV.", err)
			return
		}
		_ = h.service.LogExport(r.Context(), claims.Subject, claims.Email, "settlement-csv", clientIP(r))
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+statement.StatementID+`.csv"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case "pdf":
		data, err := settlementPDF(statement)
		if err != nil {
			h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to build PDF.", err)
			return
		}
		_ = h.service.LogExport(r.Context(), claims.Subject, claims.Email, "settlement-pdf", clientIP(r))
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+statement.StatementID+`.pdf"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	default:
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "format must be json, csv, or pdf.", nil)
	}
}

func settlementCSV(s Settlement) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	_ = w.Write([]string{"statement_id", s.StatementID})
	_ = w.Write([]string{"org", s.OrgName, "period", s.From.Format("2006-01-02") + " to " + s.To.Format("2006-01-02"), "generated_at", s.GeneratedAt})
	_ = w.Write([]string{"currency", "gross", "fees", "refunds", "fee_reversals", "net_settled", "opening_balance", "closing_balance"})
	for _, b := range s.Blocks {
		_ = w.Write([]string{b.Currency, b.Gross, b.Fees, b.Refunds, b.FeeReversals, b.NetSettled, b.OpeningBalance, b.ClosingBalance})
	}
	_ = w.Write([]string{"entry_id", "type", "direction", "amount", "currency", "order_id", "description", "created_at"})
	for _, b := range s.Blocks {
		for _, e := range b.Entries {
			_ = w.Write([]string{e.ID, e.Type, e.Direction, e.Amount, e.Currency, e.OrderID, e.Description, e.CreatedAt})
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// settlementPDF renders a letterhead statement (gofpdf/fpdf: pure Go, no
// cgo, maintained fork of gofpdf — the lightest option that does tables,
// headers/footers, and page numbers without native deps).
func settlementPDF(s Settlement) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetHeaderFunc(func() {
		pdf.SetFont("Helvetica", "B", 16)
		pdf.CellFormat(0, 8, s.BusinessName, "", 1, "L", false, 0, "")
		if s.BusinessName != s.OrgName {
			pdf.SetFont("Helvetica", "", 10)
			pdf.CellFormat(0, 5, s.OrgName, "", 1, "L", false, 0, "")
		}
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(0, 5, "Settlement statement "+s.StatementID, "", 1, "L", false, 0, "")
		pdf.CellFormat(0, 5, fmt.Sprintf("Period %s to %s (%s) · Generated %s",
			s.From.Format("2006-01-02"), s.To.Format("2006-01-02"), s.Timezone, s.GeneratedAt), "", 1, "L", false, 0, "")
		pdf.Ln(3)
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("Helvetica", "", 8)
		pdf.CellFormat(0, 10, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	pdf.AliasNbPages("{nb}")
	for _, b := range s.Blocks {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "B", 13)
		pdf.CellFormat(0, 7, "Currency: "+b.Currency, "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		summary := [][]string{
			{"Gross", b.Gross}, {"Fees", b.Fees}, {"Refunds", b.Refunds},
			{"Fee reversals", b.FeeReversals}, {"Net settled", b.NetSettled},
			{"Opening balance", b.OpeningBalance}, {"Closing balance", b.ClosingBalance},
		}
		for _, row := range summary {
			bold := row[0] == "Net settled"
			pdf.SetFont("Helvetica", map[bool]string{true: "B", false: ""}[bold], 10)
			pdf.CellFormat(60, 6, row[0], "1", 0, "L", false, 0, "")
			pdf.CellFormat(60, 6, row[1]+" "+b.Currency, "1", 1, "R", false, 0, "")
		}
		if len(b.ByProvider) > 0 {
			pdf.Ln(3)
			pdf.SetFont("Helvetica", "B", 11)
			pdf.CellFormat(0, 6, "By provider", "", 1, "L", false, 0, "")
			pdf.SetFont("Helvetica", "B", 9)
			for _, hcol := range []string{"Provider", "Gross", "Fees", "Count"} {
				pdf.CellFormat(45, 6, hcol, "1", 0, "C", false, 0, "")
			}
			pdf.Ln(-1)
			pdf.SetFont("Helvetica", "", 9)
			for _, sp := range b.ByProvider {
				pdf.CellFormat(45, 6, sp.Provider, "1", 0, "L", false, 0, "")
				pdf.CellFormat(45, 6, sp.Gross, "1", 0, "R", false, 0, "")
				pdf.CellFormat(45, 6, sp.Fees, "1", 0, "R", false, 0, "")
				pdf.CellFormat(45, 6, fmt.Sprintf("%d", sp.Count), "1", 1, "R", false, 0, "")
			}
		}
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(0, 6, fmt.Sprintf("Itemized entries (%d)", len(b.Entries)), "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 8)
		widths := []float64{22, 34, 26, 34, 64}
		for i, hcol := range []string{"Date", "Type", "Amount", "Order", "Description"} {
			pdf.CellFormat(widths[i], 6, hcol, "1", 0, "C", false, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetFont("Helvetica", "", 8)
		for _, e := range b.Entries {
			desc := e.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			order := e.OrderID
			if len(order) > 8 {
				order = order[:8]
			}
			pdf.CellFormat(widths[0], 5, e.CreatedAt[:10], "1", 0, "L", false, 0, "")
			pdf.CellFormat(widths[1], 5, truncateASCII(e.Type, 20), "1", 0, "L", false, 0, "")
			pdf.CellFormat(widths[2], 5, e.Amount, "1", 0, "R", false, 0, "")
			pdf.CellFormat(widths[3], 5, order, "1", 0, "L", false, 0, "")
			pdf.CellFormat(widths[4], 5, truncateASCII(desc, 44), "1", 1, "L", false, 0, "")
		}
		if s.Truncated {
			pdf.SetFont("Helvetica", "I", 9)
			pdf.CellFormat(0, 6, "Entry list truncated — narrow the range or export CSV for the full list.", "", 1, "L", false, 0, "")
		}
	}
	buf := &bytes.Buffer{}
	if err := pdf.Output(buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func truncateASCII(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
