// Command seed-analytics generates a synthetic analytics dataset (default
// 200 orgs / 1M orders + history + ledger) for Block 6 performance proof,
// then times the top dashboard queries (and EXPLAINs two of them).
//
// It writes clearly-marked rows (name prefix analytics-seed-) and deletes
// them first, so reruns are idempotent. NEVER run against production.
//
//	go run ./cmd/seed-analytics -orgs 200 -orders 1000000 -payer-secret dev-secret
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx"
	"lipago/internal/modules/analytics"
	"lipago/internal/platform/config"
)

const prefix = "analytics-seed-"

func main() {
	var (
		orgs        = flag.Int("orgs", 200, "organizations to seed")
		orders      = flag.Int("orders", 1000000, "payment orders to seed")
		payerSecret = flag.String("payer-secret", "", "HMAC key for payer_hash (required)")
		bench       = flag.Bool("bench", true, "time top dashboard queries + EXPLAIN after seeding")
		seed        = flag.Bool("seed", true, "seed data first (false = bench existing data only)")
		dbURLFlag   = flag.String("database-url", os.Getenv("DATABASE_URL"), "postgres connection string")
	)
	flag.Parse()
	if *payerSecret == "" {
		config.LoadDotEnv(".env")
		*payerSecret = os.Getenv("ANALYTICS_PAYER_SECRET")
	}
	if *payerSecret == "" {
		log.Fatal("-payer-secret (or ANALYTICS_PAYER_SECRET) is required")
	}
	if strings.TrimSpace(*dbURLFlag) == "" {
		config.LoadDotEnv(".env")
		*dbURLFlag = os.Getenv("DATABASE_URL")
	}
	if strings.TrimSpace(*dbURLFlag) == "" {
		log.Fatal("DATABASE_URL or -database-url is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	connCfg, err := pgx.ParseConnectionString(*dbURLFlag)
	if err != nil {
		log.Fatalf("parse database url: %v", err)
	}
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: connCfg, MaxConnections: 8, AcquireTimeout: 10 * time.Second})
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if !*seed {
		if *bench {
			benchQueries(ctx, pool)
		}
		return
	}
	cleanup(ctx, pool)
	if _, err := pool.ExecEx(ctx, `ANALYZE app.payment_orders`, nil); err != nil {
		log.Printf("analyze (non-fatal): %v", err)
	}
	if _, err := pool.ExecEx(ctx, `ANALYZE app.payment_status_history`, nil); err != nil {
		log.Printf("analyze (non-fatal): %v", err)
	}
	if _, err := pool.ExecEx(ctx, `ANALYZE app.payment_ledger_entries`, nil); err != nil {
		log.Printf("analyze (non-fatal): %v", err)
	}
	if !*seed {
		if *bench {
			benchQueries(ctx, pool)
		}
		return
	}
	log.Printf("seeding %d orgs / %d orders...", *orgs, *orders)
	start := time.Now()
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.organizations (name, slug, kyc_status)
		SELECT $1 || g, $1 || g, 'verified' FROM generate_series(1, $2) g`, nil, prefix, *orgs); err != nil {
		log.Fatalf("seed orgs: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_apps (name, org_id, fee_type, fee_percent)
		SELECT $1 || o.id::text, o.id, 'percentage', 2 FROM app.organizations o WHERE o.name LIKE $1 || '%'`, nil, prefix); err != nil {
		log.Fatalf("seed apps: %v", err)
	}
	// Orders: 90-day spread, weighted statuses, 95% live, mixed channels.
	// external_reference tags seed rows so history + cleanup stay scoped.
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_orders
		  (app_id, provider, amount, currency, buyer_phone, status, environment, channel, failure_code, payer_hash, provider_latency_ms, external_reference, created_at, updated_at, first_paid_at)
		SELECT a.id,
		       (ARRAY['sonicpesa','sandbox'])[1 + (g % 20 = 0)::int],
		       (1000 + (random() * 99000))::numeric(18,2),
		       (ARRAY['TZS','TZS','TZS','USD'])[(1 + floor(random()*4))::int],
		       '+2557' || lpad((10000000 + floor(random()*89999999))::text, 8, '0'),
		       CASE WHEN r < 0.70 THEN 'paid' WHEN r < 0.80 THEN 'failed' WHEN r < 0.85 THEN 'expired' ELSE 'pending' END,
		       CASE WHEN random() < 0.95 THEN 'live' ELSE 'sandbox' END,
		       (ARRAY['M-PESA','AIRTEL','TIGO','OTHER'])[(1 + floor(random()*4))::int],
		       CASE WHEN r >= 0.70 AND r < 0.80 THEN 'provider_declined' WHEN r >= 0.80 AND r < 0.85 THEN 'expired' END,
		       encode(hmac('lipago-payer-v1:' || '+2557' || lpad((10000000 + floor(random()*89999999))::text, 8, '0'), $1, 'sha256'), 'hex'),
		       (50 + floor(random()*900))::bigint,
		       $4 || g,
		       NOW() - (floor(random()*90) || ' days')::interval - (floor(random()*86400) || ' seconds')::interval,
		       NOW(),
		       CASE WHEN r < 0.70 THEN NOW() - (floor(random()*90) || ' days')::interval - (floor(random()*86400) || ' seconds')::interval + ((30 + floor(random()*570)) || ' seconds')::interval END
		FROM generate_series(1, $2) g,
		     (SELECT random() AS r) AS rr,
		     (SELECT id, row_number() OVER (ORDER BY id) AS rn, COUNT(*) OVER () AS n
		      FROM app.payment_apps WHERE name LIKE $3 || '%') a
		WHERE a.rn = 1 + (g % a.n)`, nil, *payerSecret, *orders, prefix, prefix); err != nil {
		log.Fatalf("seed orders: %v", err)
	}
	// NOTE: payer phones are random per row, so repeat rate ≈ 0 on seed
	// data; the second batch below reuses 5k phones to model repeats.
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_orders
		  (app_id, provider, amount, currency, buyer_phone, status, environment, channel, payer_hash, external_reference, created_at, updated_at)
		SELECT a.id, 'sonicpesa', 5000, 'TZS', '+2557' || lpad((70000000 + (g % 5000))::text, 8, '0'),
		       'paid', 'live', 'M-PESA',
		       encode(hmac('lipago-payer-v1:2557' || lpad((70000000 + (g % 5000))::text, 8, '0'), $1, 'sha256'), 'hex'),
		       $3 || 'repeat-' || g,
		       NOW() - ((g % 30) || ' days')::interval, NOW()
		FROM generate_series(1, 20000) g,
		     (SELECT id, row_number() OVER (ORDER BY id) AS rn, COUNT(*) OVER () AS n
		      FROM app.payment_apps WHERE name LIKE $2 || '%') a
		WHERE a.rn = 1 + (g % a.n)`, nil, *payerSecret, prefix, prefix); err != nil {
		log.Fatalf("seed repeat orders: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_status_history (payment_order_id, to_status, source, created_at)
		SELECT id, 'pending', 'create_order', created_at FROM app.payment_orders WHERE external_reference LIKE $1 || '%'`, nil, prefix); err != nil {
		log.Printf("history (non-fatal): %v", err)
	}
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_status_history (payment_order_id, from_status, to_status, source, created_at)
		SELECT id, 'pending', 'paid', 'webhook', created_at + ((30 + floor(random()*570)) || ' seconds')::interval
		FROM app.payment_orders WHERE external_reference LIKE $1 || '%' AND status = 'paid'`, nil, prefix); err != nil {
		log.Printf("paid history (non-fatal): %v", err)
	}
	// Ledger credits + fees for paid seed orders (money rollups stay honest).
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, created_at)
		SELECT app_id, id, 'payment_credit', 'credit', amount, currency, created_at
		FROM app.payment_orders WHERE external_reference LIKE $1 || '%' AND status = 'paid'`, nil, prefix); err != nil {
		log.Printf("ledger credits (non-fatal): %v", err)
	}
	if _, err := pool.ExecEx(ctx, `
		INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, created_at)
		SELECT app_id, id, 'platform_fee_debit', 'debit', (amount * 0.02)::numeric(18,2), currency, created_at
		FROM app.payment_orders WHERE external_reference LIKE $1 || '%' AND status = 'paid'`, nil, prefix); err != nil {
		log.Printf("ledger fees (non-fatal): %v", err)
	}
	log.Printf("seed done in %s", time.Since(start).Round(time.Second))

	if *bench {
		benchQueries(ctx, pool)
	}
}

func cleanup(ctx context.Context, pool *pgx.ConnPool) {
	_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_ledger_entries WHERE app_id IN (SELECT id FROM app.payment_apps WHERE name LIKE $1 || '%')`, nil, prefix)
	_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_status_history WHERE payment_order_id IN (SELECT id FROM app.payment_orders WHERE external_reference LIKE $1 || '%')`, nil, prefix)
	_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_orders WHERE external_reference LIKE $1 || '%'`, nil, prefix)
	_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_apps WHERE name LIKE $1 || '%'`, nil, prefix)
	_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE name LIKE $1 || '%'`, nil, prefix)
}

func benchQueries(ctx context.Context, pool *pgx.ConnPool) {
	repo := analytics.NewPostgresRepository(pool)
	svc := analytics.NewService(repo, nil)
	now := time.Now().UTC()
	p := analytics.Params{
		From: now.Add(-30 * 24 * time.Hour), To: now,
		Granularity: analytics.GranularityDay, Page: 1, PerPage: 50,
		DormantDays: 30, ChurnDropPct: 50, ChurnWindowDays: 14, StuckMinutes: 30,
	}
	timeIt := func(name string, fn func() error) {
		best := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			if err := fn(); err != nil {
				log.Printf("bench %s: ERROR %v", name, err)
				return
			}
			if took := time.Since(start); took < best {
				best = took
			}
		}
		log.Printf("bench %s: best-of-3 %s", name, best.Round(time.Millisecond))
	}
	timeIt("overview-30d", func() error { _, err := repo.Overview(ctx, p); return err })
	timeIt("overview-30d-cached", func() error { _, err := svc.Overview(ctx, p); return err })
	timeIt("top-merchants", func() error { _, err := repo.TopMerchants(ctx, p, "tx_count"); return err })
	timeIt("top-merchants-cached", func() error { _, err := svc.TopMerchants(ctx, p, "tx_count"); return err })
	timeIt("customers-repeat", func() error {
		_, err := repo.MerchantCustomers(ctx, p, "live", true, func(s string) string { return s })
		return err
	})
	// EXPLAIN proof on the two hottest shapes.
	for _, q := range []struct {
		name, sql string
	}{
		{"overview-kpis", `SELECT COUNT(*), COUNT(*) FILTER (WHERE status='paid') FROM app.payment_orders WHERE created_at >= $1 AND created_at < $2 AND environment='live'`},
		{"top-merchants", `SELECT a.org_id, COUNT(*) FROM app.payment_orders o JOIN app.payment_apps a ON a.id=o.app_id WHERE o.created_at >= $1 AND o.created_at < $2 AND o.environment='live' GROUP BY 1`},
	} {
		rows, err := pool.QueryEx(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+q.sql, nil, p.From, p.To)
		if err != nil {
			log.Printf("explain %s: ERROR %v", q.name, err)
			continue
		}
		var plan string
		for rows.Next() {
			var chunk string
			_ = rows.Scan(&chunk)
			plan += chunk
		}
		rows.Close()
		log.Printf("explain %s: total=%.1fms", q.name, extractExecMs(plan))
	}
}

func extractExecMs(plan string) float64 {
	// Minimal JSON scrape: last "Execution Time": N occurrence.
	idx := -1
	for i := 0; i < len(plan)-17; i++ {
		if plan[i:i+17] == "\"Execution Time\":" {
			idx = i + 17
		}
	}
	if idx < 0 {
		return -1
	}
	var ms float64
	fmt.Sscanf(plan[idx:], "%f", &ms)
	return ms
}

func strings_TrimSpace(s string) string {
	return strings.TrimSpace(s)
}
