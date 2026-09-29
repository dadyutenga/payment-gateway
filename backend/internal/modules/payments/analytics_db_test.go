package payments

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx"
)

// TestAnalyticsRollupParity rebuilds rollups from a fixture dataset with
// known answers: expired/failed/sandbox rows, mixed currencies, and an EAT
// midnight boundary. Requires DATABASE_URL (dev/CI postgres); skipped
// otherwise. It also asserts rebuild idempotency (refresh twice, same
// rows).
func TestAnalyticsRollupParity(t *testing.T) {
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
		nil, "analytics-test-"+suffix, "analytics-test-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.payment_apps (name, org_id, fee_type, fee_percent) VALUES ($1, $2::uuid, 'percentage', 2.5) RETURNING id::text`,
		nil, "analytics-app-"+suffix, orgID).Scan(&appID); err != nil {
		t.Fatalf("insert app: %v", err)
	}
	defer func() {
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_ledger_entries WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_orders WHERE app_id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_apps WHERE id = $1::uuid`, nil, appID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE id = $1::uuid`, nil, orgID)
	}()

	type fixture struct {
		status, env, currency, amount, channel, failure string
		created                                        time.Time
		paidAfter                                      time.Duration // 0 = never paid
	}
	day := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	orders := []fixture{
		{"paid", "live", "TZS", "10000", "M-PESA", "", day, 90 * time.Second},
		{"paid", "live", "TZS", "20000", "AIRTEL", "", day.Add(5 * time.Minute), 300 * time.Second},
		{"paid", "live", "USD", "50", "TIGO", "", day.Add(10 * time.Minute), 60 * time.Second},
		{"failed", "live", "TZS", "5000", "", "provider_declined", day.Add(15 * time.Minute), 0},
		{"expired", "live", "TZS", "7000", "", "expired", day.Add(20 * time.Minute), 0},
		{"paid", "sandbox", "TZS", "999999", "M-PESA", "", day.Add(25 * time.Minute), 30 * time.Second},
		{"paid", "live", "TZS", "5000", "", "", time.Date(2026, 9, 28, 20, 30, 0, 0, time.UTC), 120 * time.Second},
		{"paid", "live", "TZS", "6000", "", "", time.Date(2026, 9, 28, 21, 30, 0, 0, time.UTC), 120 * time.Second},
	}
	var paidTZS string
	for i, o := range orders {
		var id string
		err := pool.QueryRowEx(ctx, `
			INSERT INTO app.payment_orders
			  (app_id, provider, amount, currency, buyer_phone, status, environment, channel, failure_code, created_at, updated_at)
			VALUES ($1::uuid, 'sandbox', $2::numeric, $3, '+255700000001', $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $8)
			RETURNING id::text`, nil,
			appID, o.amount, o.currency, o.status, o.env, o.channel, o.failure, o.created).Scan(&id)
		if err != nil {
			t.Fatalf("insert order %d: %v", i, err)
		}
		if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_status_history (payment_order_id, to_status, source, created_at) VALUES ($1::uuid, 'pending', 'create_order', $2)`,
			nil, id, o.created); err != nil {
			t.Fatalf("insert history %d: %v", i, err)
		}
		if o.status != "pending" && o.paidAfter == 0 {
			if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_status_history (payment_order_id, from_status, to_status, source, created_at) VALUES ($1::uuid, 'pending', $2, 'webhook', $3)`,
				nil, id, o.status, o.created.Add(time.Second)); err != nil {
				t.Fatalf("insert final history %d: %v", i, err)
			}
		}
		if o.paidAfter > 0 {
			if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_status_history (payment_order_id, from_status, to_status, source, created_at) VALUES ($1::uuid, 'pending', 'paid', 'webhook', $2)`,
				nil, id, o.created.Add(o.paidAfter)); err != nil {
				t.Fatalf("insert paid history %d: %v", i, err)
			}
			if o.env == "live" && o.currency == "TZS" && paidTZS == "" {
				paidTZS = id
			}
		}
	}
	// Ledger: one fee + one refund on day 28 (EAT).
	if _, err := pool.ExecEx(ctx, `INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, created_at) VALUES
		($1::uuid, $2::uuid, 'platform_fee_debit', 'debit', 500, 'TZS', $3),
		($1::uuid, $2::uuid, 'refund_debit', 'debit', 10000, 'TZS', $3)`,
		nil, appID, paidTZS, day.Add(time.Hour)); err != nil {
		t.Fatalf("insert ledger: %v", err)
	}

	day28 := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	day29 := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	if err := repo.RefreshAnalyticsRollups(ctx, day28, day29); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	type dailyRow struct {
		day, env, provider, channel, currency               string
		created, succeeded, failed, expired                 int
		gross                                              string
		p50, p90                                           *float64
	}
	var rows []dailyRow
	dbRows, err := pool.QueryEx(ctx, `SELECT day::text, environment, provider, channel, currency, created, succeeded, failed, expired, gross::text, p50_ttp_s, p90_ttp_s
		FROM app.analytics_daily_app WHERE app_id = $1::uuid ORDER BY day, currency, channel`, nil, appID)
	if err != nil {
		t.Fatalf("read daily: %v", err)
	}
	for dbRows.Next() {
		var r dailyRow
		if err := dbRows.Scan(&r.day, &r.env, &r.provider, &r.channel, &r.currency, &r.created, &r.succeeded, &r.failed, &r.expired, &r.gross, &r.p50, &r.p90); err != nil {
			dbRows.Close()
			t.Fatalf("scan daily: %v", err)
		}
		rows = append(rows, r)
	}
	dbRows.Close()
	if err := dbRows.Err(); err != nil {
		t.Fatalf("iterate daily: %v", err)
	}
	byKey := map[string]dailyRow{}
	for _, r := range rows {
		byKey[r.day+"|"+r.env+"|"+r.channel+"|"+r.currency] = r
	}

	mpesa, ok := byKey["2026-09-28|live|M-PESA|TZS"]
	if !ok {
		t.Fatalf("missing M-PESA live TZS grain: %+v", rows)
	}
	if mpesa.created != 1 || mpesa.succeeded != 1 || mpesa.gross != "10000.00" {
		t.Fatalf("M-PESA grain wrong: %+v", mpesa)
	}
	if mpesa.p50 == nil || *mpesa.p50 != 90 || mpesa.p90 == nil || *mpesa.p90 != 90 {
		t.Fatalf("M-PESA TTP wrong: p50=%v p90=%v", mpesa.p50, mpesa.p90)
	}
	other, ok := byKey["2026-09-28|live|OTHER|TZS"]
	if !ok {
		t.Fatalf("missing OTHER live TZS grain: %+v", rows)
	}
	// o4 failed + o5 expired + o7 paid(20:30Z = EAT 28th).
	if other.created != 3 || other.succeeded != 1 || other.failed != 1 || other.expired != 1 || other.gross != "5000.00" {
		t.Fatalf("OTHER grain wrong: %+v", other)
	}
	usd, ok := byKey["2026-09-28|live|TIGO|USD"]
	if !ok || usd.gross != "50.00" || usd.succeeded != 1 {
		t.Fatalf("USD grain wrong/missing: %+v", rows)
	}
	sbx, ok := byKey["2026-09-28|sandbox|M-PESA|TZS"]
	if !ok || sbx.gross != "999999.00" {
		t.Fatalf("sandbox must live in its own grain: %+v", rows)
	}
	for _, r := range rows {
		if r.env == "live" && r.gross == "999999" {
			t.Fatalf("sandbox leaked into live: %+v", r)
		}
	}
	d29, ok := byKey["2026-09-29|live|OTHER|TZS"]
	if !ok || d29.created != 1 || d29.succeeded != 1 || d29.gross != "6000.00" {
		t.Fatalf("EAT boundary wrong (21:30Z must land on the 29th): %+v", rows)
	}

	var fees, refundTotal string
	var refunds int
	if err := pool.QueryRowEx(ctx, `SELECT fees::text, refunds, refund_total::text FROM app.analytics_daily_app_money WHERE app_id = $1::uuid AND day = '2026-09-28' AND currency = 'TZS'`,
		nil, appID).Scan(&fees, &refunds, &refundTotal); err != nil {
		t.Fatalf("read money rollup: %v", err)
	}
	if fees != "500.00" || refunds != 1 || refundTotal != "10000.00" {
		t.Fatalf("money rollup wrong: fees=%s refunds=%d total=%s", fees, refunds, refundTotal)
	}

	// Idempotency: refresh again, counts must be identical.
	if err := repo.RefreshAnalyticsRollups(ctx, day28, day29); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	var dailyCount, moneyCount int
	if err := pool.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.analytics_daily_app WHERE app_id = $1::uuid`, nil, appID).Scan(&dailyCount); err != nil {
		t.Fatalf("count daily: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.analytics_daily_app_money WHERE app_id = $1::uuid`, nil, appID).Scan(&moneyCount); err != nil {
		t.Fatalf("count money: %v", err)
	}
	if dailyCount != len(rows) || moneyCount != 1 {
		t.Fatalf("rebuild not idempotent: daily=%d (want %d) money=%d", dailyCount, len(rows), moneyCount)
	}
}
