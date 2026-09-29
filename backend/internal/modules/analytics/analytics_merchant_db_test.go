package analytics

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx"
)

func merchantTestPool(t *testing.T, ctx context.Context) *pgx.ConnPool {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — needs postgres")
	}
	connCfg, err := pgx.ParseConnectionString(dbURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: connCfg, MaxConnections: 4, AcquireTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return pool
}

// TestMerchantSettlementEqualsLedger asserts statement totals equal an
// independent ledger recomputation, plus CSV/PDF smoke.
func TestMerchantSettlementEqualsLedger(t *testing.T) {
	ctx := context.Background()
	pool := merchantTestPool(t, ctx)
	defer pool.Close()
	repo := NewPostgresRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, appID, orderID string
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug, kyc_status, business_name) VALUES ($1, $2, 'verified', 'Stmt Ltd') RETURNING id::text`,
		nil, "stmt-test-"+suffix, "stmt-test-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.payment_apps (name, org_id, fee_type, fee_percent) VALUES ($1, $2::uuid, 'percentage', 2.5) RETURNING id::text`,
		nil, "stmt-app-"+suffix, orgID).Scan(&appID); err != nil {
		t.Fatalf("insert app: %v", err)
	}
	defer func() {
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_ledger_entries WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_orders WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_apps WHERE id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE id = $1::uuid`, nil, orgID)
	}()

	day := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	if err := pool.QueryRowEx(ctx, `
		INSERT INTO app.payment_orders (app_id, provider, amount, currency, buyer_phone, status, environment, created_at, updated_at)
		VALUES ($1::uuid, 'sandbox', 10000, 'TZS', '+255700000001', 'paid', 'live', $2, $2) RETURNING id::text`,
		nil, appID, day).Scan(&orderID); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	entries := []struct{ typ, dir, amount string }{
		{"payment_credit", "credit", "10000"},
		{"platform_fee_debit", "debit", "250"},
		{"refund_debit", "debit", "2000"},
		{"refund_fee_reversal_credit", "credit", "50"},
		{"withdrawal_debit", "debit", "1000"},
	}
	for _, e := range entries {
		var orderRef any
		if e.typ == "withdrawal_debit" {
			orderRef = nil
		} else {
			orderRef = orderID
		}
		if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, created_at)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5::numeric, 'TZS', $6)`,
			nil, appID, orderRef, e.typ, e.dir, e.amount, day.Add(time.Hour)); err != nil {
			t.Fatalf("insert ledger %s: %v", e.typ, err)
		}
	}

	p := Params{
		From: day.Add(-time.Hour), To: day.Add(48 * time.Hour),
		Granularity: GranularityDay, OrgIDs: []string{orgID}, Page: 1, PerPage: 50,
	}
	stmt, err := repo.MerchantSettlements(ctx, p, "live", "TZS", 500)
	if err != nil {
		t.Fatalf("settlements: %v", err)
	}
	if len(stmt.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(stmt.Blocks))
	}
	b := stmt.Blocks[0]
	// Independent recomputation straight from the ledger.
	var gross, fees, refunds, reversals string
	if err := pool.QueryRowEx(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE entry_type = 'payment_credit'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE entry_type = 'platform_fee_debit'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE entry_type = 'refund_debit'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE entry_type = 'refund_fee_reversal_credit'), 0)::text
		FROM app.payment_ledger_entries WHERE app_id = $1::uuid AND currency = 'TZS'`,
		nil, appID).Scan(&gross, &fees, &refunds, &reversals); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if b.Gross != gross || b.Fees != fees || b.Refunds != refunds || b.FeeReversals != reversals {
		t.Fatalf("statement != ledger: got %+v want %s/%s/%s/%s", b, gross, fees, refunds, reversals)
	}
	if b.NetSettled != "7800.00" {
		t.Fatalf("net = %q, want 7800.00", b.NetSettled)
	}
	if len(b.Entries) != 5 {
		t.Fatalf("want 5 itemized entries, got %d", len(b.Entries))
	}
	// Closing = all credits - all debits = 10000+50 - (250+2000+1000) = 6800.
	if b.ClosingBalance != "6800.00" {
		t.Fatalf("closing = %q, want 6800.00", b.ClosingBalance)
	}

	svc := NewService(repo, nil)
	_ = svc
	// CSV/PDF smoke via the settlement builders.
	csvBytes, err := settlementCSV(stmt)
	if err != nil || !bytes.Contains(csvBytes, []byte(stmt.StatementID)) {
		t.Fatalf("csv smoke: %v", err)
	}
	pdfBytes, err := settlementPDF(stmt)
	if err != nil || !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("pdf smoke: %v len=%d", err, len(pdfBytes))
	}
}
