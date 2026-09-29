package analytics

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx"

	"lipago/internal/modules/payments"
)

// TestOverviewRollupLiveEquivalence proves the rollup+live merge equals
// hand-computed truth (counts, rates, money, TTP, series), including the
// live-fallback path when a rollup day is missing.
func TestOverviewRollupLiveEquivalence(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — needs postgres")
	}
	ctx := context.Background()
	connCfg, err := pgx.ParseConnectionString(dbURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: connCfg, MaxConnections: 4, AcquireTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	repo := NewPostgresRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, appID string
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug, kyc_status) VALUES ($1, $2, 'verified') RETURNING id::text`,
		nil, "ov-test-"+suffix, "ov-test-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.payment_apps (name, org_id, fee_type, fee_percent) VALUES ($1, $2::uuid, 'percentage', 2) RETURNING id::text`,
		nil, "ov-app-"+suffix, orgID).Scan(&appID); err != nil {
		t.Fatalf("insert app: %v", err)
	}
	defer func() {
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_ledger_entries WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_orders WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.analytics_daily_app WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.analytics_daily_app_money WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.analytics_hourly_app WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_apps WHERE id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE id = $1::uuid`, nil, orgID)
	}()

	// EAT days: o1+o2 on the 27th, o3+o4 on the 28th, o5 "today".
	// (Test clocks "today" as 2026-09-29 EAT via explicit bounds below.)
	type fix struct {
		status, currency, amount, channel string
		created                           time.Time
		ttp                               time.Duration
		fee                               string
	}
	fixtures := []fix{
		{"paid", "TZS", "10000", "M-PESA", time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC), 90 * time.Second, "200"},
		{"failed", "TZS", "3000", "AIRTEL", time.Date(2026, 9, 26, 22, 30, 0, 0, time.UTC), 0, ""},
		{"paid", "USD", "50", "TIGO", time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), 120 * time.Second, "1"},
		{"expired", "TZS", "7000", "", time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC), 0, ""},
	}
	var paidIDs []string
	for i, f := range fixtures {
		var id string
		var firstPaid any
		if f.ttp > 0 {
			firstPaid = f.created.Add(f.ttp)
		}
		if err := pool.QueryRowEx(ctx, `
			INSERT INTO app.payment_orders (app_id, provider, amount, currency, buyer_phone, status, environment, channel, first_paid_at, created_at, updated_at)
			VALUES ($1::uuid, 'sandbox', $2::numeric, $3, '+255700000001', $4, 'live', NULLIF($5, ''), $6, $7, $7)
			RETURNING id::text`, nil,
			appID, f.amount, f.currency, f.status, f.channel, firstPaid, f.created).Scan(&id); err != nil {
			t.Fatalf("insert order %d: %v", i, err)
		}
		if f.status == "paid" {
			paidIDs = append(paidIDs, id)
			if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, created_at)
				VALUES ($1::uuid, $2::uuid, 'platform_fee_debit', 'debit', $3::numeric, $4, $5)`,
				nil, appID, id, f.fee, f.currency, f.created); err != nil {
				t.Fatalf("insert fee %d: %v", i, err)
			}
		}
	}
	_ = paidIDs

	// Roll up the 27th AND 28th (to is exclusive); then delete the 28th
	// to prove the live-fallback path returns identical totals.
	day27 := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	day29 := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	payRepo := payments.NewPostgresRepository(pool)
	if err := payRepo.RefreshAnalyticsRollups(ctx, day27, day29); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Window: EAT 27th 00:00 → 29th 00:00 (o5 excluded: created "today"
	// relative to the real clock — instead assert on 27+28 only).
	from := time.Date(2026, 9, 26, 21, 0, 0, 0, time.UTC) // 27th 00:00 EAT
	to := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)   // 29th 00:00 EAT
	p := Params{
		From: from, To: to, Granularity: GranularityDay,
		OrgIDs: []string{orgID}, Page: 1, PerPage: 50,
		DormantDays: 30, ChurnDropPct: 50, ChurnWindowDays: 14, StuckMinutes: 30,
	}
	out, err := repo.Overview(ctx, p)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.TxCount != 4 {
		t.Fatalf("tx = %d, want 4", out.TxCount)
	}
	if out.SuccessRate == nil || *out.SuccessRate != 0.5 {
		t.Fatalf("success = %v, want 0.5", out.SuccessRate)
	}
	if out.AbandonmentRate == nil || *out.AbandonmentRate != 0.25 {
		t.Fatalf("abandon = %v, want 0.25", out.AbandonmentRate)
	}
	if out.TPV["TZS"] != "10000.00" || out.TPV["USD"] != "50.00" {
		t.Fatalf("tpv = %v", out.TPV)
	}
	if out.Revenue["TZS"] != "200.00" || out.Revenue["USD"] != "1.00" {
		t.Fatalf("revenue = %v", out.Revenue)
	}
	// TTP over [90s, 120s] → median 105, p90 117.
	if out.MedianTTPS == nil || *out.MedianTTPS != 105 {
		t.Fatalf("median ttp = %v, want 105", out.MedianTTPS)
	}
	if out.P90TTPS == nil || *out.P90TTPS != 117 {
		t.Fatalf("p90 ttp = %v, want 117", out.P90TTPS)
	}
	if len(out.Series) != 2 {
		t.Fatalf("series buckets = %d, want 2: %+v", len(out.Series), out.Series)
	}
	if out.Series[0].Bucket != "2026-09-27" || out.Series[0].TxCount != 2 {
		t.Fatalf("series[0] = %+v", out.Series[0])
	}
	if out.Series[1].Bucket != "2026-09-28" || out.Series[1].TxCount != 2 {
		t.Fatalf("series[1] = %+v", out.Series[1])
	}
	if out.ActiveMerchants != 1 {
		t.Fatalf("active = %d, want 1", out.ActiveMerchants)
	}
	// Fallback proof: drop the 28th rollup rows — totals must not move.
	if _, err := pool.ExecEx(ctx, `DELETE FROM app.analytics_daily_app WHERE app_id = $1::uuid AND day = '2026-09-28'`, nil, appID); err != nil {
		t.Fatalf("drop day 28 rollup: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `DELETE FROM app.analytics_daily_app_money WHERE app_id = $1::uuid AND day = '2026-09-28'`, nil, appID); err != nil {
		t.Fatalf("drop day 28 money: %v", err)
	}
	fallback, err := repo.Overview(ctx, p)
	if err != nil {
		t.Fatalf("overview after drop: %v", err)
	}
	if fallback.TxCount != out.TxCount || fallback.TPV["TZS"] != out.TPV["TZS"] || fallback.TPV["USD"] != out.TPV["USD"] {
		t.Fatalf("fallback moved totals: %+v vs %+v", fallback, out)
	}
	if len(fallback.Series) != 2 || fallback.Series[1].TxCount != 2 {
		t.Fatalf("fallback series wrong: %+v", fallback.Series)
	}
}
