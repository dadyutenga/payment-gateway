package orgs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx"
)

// TestAccountKindBackfillMigration exercises 000057 against a scratch
// database with a fixture dataset covering: a plain merchant (untouched),
// an unambiguous creator lookalike (flipped), a contradictory org
// (flagged, never guessed), an explicit creator (untouched), and a bare
// merchant (untouched). It also proves up is idempotent and down only
// removes scaffolding (flipped kinds stay flipped — they are data).
//
// It needs a reachable Postgres (same credentials as DATABASE_URL) and is
// skipped otherwise. It NEVER touches the development database: all work
// happens in a scratch database that is dropped at the end.
func TestAccountKindBackfillMigration(t *testing.T) {
	maintenanceURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if maintenanceURL == "" {
		maintenanceURL = "postgresql://postgres:123456789@localhost:5432/postgres?sslmode=disable"
	}
	scratchName := "payments_gateway_backfilltest"
	scratchURL := strings.Replace(maintenanceURL, "/postgres?", "/"+scratchName+"?", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	adminPool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: mustParseConn(t, maintenanceURL), MaxConnections: 1})
	if err != nil {
		t.Skipf("no postgres for migration test: %v", err)
	}
	defer adminPool.Close()
	if _, err := adminPool.ExecEx(ctx, `DROP DATABASE IF EXISTS `+scratchName, nil); err != nil {
		t.Skipf("cannot reset scratch database: %v", err)
	}
	if _, err := adminPool.ExecEx(ctx, `CREATE DATABASE `+scratchName, nil); err != nil {
		t.Skipf("cannot create scratch database: %v", err)
	}
	defer func() {
		_, _ = adminPool.ExecEx(context.Background(), `DROP DATABASE IF EXISTS `+scratchName, nil)
	}()

	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: mustParseConn(t, scratchURL), MaxConnections: 2})
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	defer pool.Close()

	migDir := filepath.Join("..", "..", "..", "migrations")
	applyUp := func(t *testing.T, maxVersion string) {
		t.Helper()
		entries, err := os.ReadDir(migDir)
		if err != nil {
			t.Fatalf("read migrations dir: %v", err)
		}
		var files []string
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
				continue
			}
			if version := strings.TrimSuffix(name, ".up.sql"); version <= maxVersion {
				files = append(files, name)
			}
		}
		sort.Strings(files)
		if len(files) == 0 {
			t.Fatalf("no migrations found up to %s", maxVersion)
		}
		for _, name := range files {
			sqlBytes, err := os.ReadFile(filepath.Join(migDir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if _, err := pool.ExecEx(ctx, string(sqlBytes), nil); err != nil {
				t.Fatalf("apply %s: %v", name, err)
			}
		}
	}

	// Base schema only — 000057 runs against the fixture below.
	applyUp(t, "000056_creator_payout_destinations")

	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := pool.ExecEx(ctx, query, nil, args...); err != nil {
			t.Fatalf("fixture exec failed: %v\nquery: %s", err, query)
		}
	}

	// Five users, five orgs.
	for i, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com", "e@example.com"} {
		exec(`INSERT INTO app.users (id, email, password_hash) VALUES ($1::uuid, $2, 'x')`,
			fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1), email)
	}
	orgIDs := map[string]string{"A": "11111111-1111-1111-1111-111111111111", "B": "22222222-2222-2222-2222-222222222222", "C": "33333333-3333-3333-3333-333333333333", "D": "44444444-4444-4444-4444-444444444444", "E": "55555555-5555-5555-5555-555555555555"}
	exec(`INSERT INTO app.organizations (id, name, slug, business_name, tin) VALUES ($1::uuid, 'Acme', 'acme', 'Acme Ltd', '123456789')`, orgIDs["A"])
	exec(`INSERT INTO app.organizations (id, name, slug, display_name, handle, bio) VALUES ($1::uuid, 'Amina page', 'amina', 'Amina Creates', 'amina.creates', 'Videos')`, orgIDs["B"])
	exec(`INSERT INTO app.organizations (id, name, slug, display_name, handle, business_name) VALUES ($1::uuid, 'Mixed', 'mixed', 'Mixed Biz', 'mixed.biz', 'Mixed Ltd')`, orgIDs["C"])
	exec(`INSERT INTO app.organizations (id, name, slug, account_kind, display_name, handle) VALUES ($1::uuid, 'Real Creator', 'real-creator', 'creator', 'Real', 'real.creator')`, orgIDs["D"])
	exec(`INSERT INTO app.organizations (id, name, slug) VALUES ($1::uuid, 'Plain', 'plain')`, orgIDs["E"])
	for i, key := range []string{"A", "B", "C", "D", "E"} {
		exec(`INSERT INTO app.org_members (org_id, user_id, role, invited_by, status) VALUES ($1::uuid, $2::uuid, 'owner', $2, 'active')`,
			orgIDs[key], fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1))
	}
	// A: business KYC. B: individual KYC + survey. C: business KYC on a handle org.
	exec(`INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url) VALUES ($1::uuid, 'Acme Ltd', '123456789', 'uploads/kyc/a.pdf')`, orgIDs["A"])
	exec(`INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url, full_name, id_type, id_number, selfie_url) VALUES ($1::uuid, '', '', 'uploads/kyc/b.pdf', 'Amina Juma', 'national_id', '19900101-00001-00001-00', 'uploads/kyc/b-selfie.jpg')`, orgIDs["B"])
	exec(`INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url) VALUES ($1::uuid, 'Mixed Ltd', '987654321', 'uploads/kyc/c.pdf')`, orgIDs["C"])
	exec(`INSERT INTO app.creator_onboarding_surveys (org_id, category, referral_source, use_cases, expected_volume_band, expected_txn_band) VALUES ($1::uuid, 'content_creator', 'social_media', '["support_tips"]', 'under_100k', 'under_50')`, orgIDs["B"])

	up057, err := os.ReadFile(filepath.Join(migDir, "000057_account_kind_backfill.up.sql"))
	if err != nil {
		t.Fatalf("read 000057 up: %v", err)
	}
	kindOf := func(orgID string) (kind, source string) {
		t.Helper()
		if err := pool.QueryRowEx(ctx, `SELECT account_kind, account_kind_source FROM app.organizations WHERE id = $1::uuid`, nil, orgID).Scan(&kind, &source); err != nil {
			t.Fatalf("read kind for %s: %v", orgID, err)
		}
		return kind, source
	}

	if _, err := pool.ExecEx(ctx, string(up057), nil); err != nil {
		t.Fatalf("apply 000057 up: %v", err)
	}

	if kind, source := kindOf(orgIDs["A"]); kind != "merchant" || source != "signup" {
		t.Errorf("A (plain merchant): got kind=%q source=%q, want merchant/signup", kind, source)
	}
	if kind, source := kindOf(orgIDs["B"]); kind != "creator" || source != "backfill_000057" {
		t.Errorf("B (creator lookalike): got kind=%q source=%q, want creator/backfill_000057", kind, source)
	}
	if kind, _ := kindOf(orgIDs["C"]); kind != "merchant" {
		t.Errorf("C (ambiguous): kind must stay merchant, got %q", kind)
	}
	if kind, source := kindOf(orgIDs["D"]); kind != "creator" || source != "signup" {
		t.Errorf("D (explicit creator): got kind=%q source=%q, want creator/signup", kind, source)
	}
	if kind, source := kindOf(orgIDs["E"]); kind != "merchant" || source != "signup" {
		t.Errorf("E (bare merchant): got kind=%q source=%q, want merchant/signup", kind, source)
	}

	var reviewCount int
	if err := pool.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.account_kind_review`, nil).Scan(&reviewCount); err != nil {
		t.Fatalf("count review rows: %v", err)
	}
	if reviewCount != 1 {
		t.Fatalf("expected exactly 1 flagged row (C), got %d", reviewCount)
	}
	var currentKind, detected, signals string
	var reviewOrg string
	if err := pool.QueryRowEx(ctx, `SELECT org_id::text, current_kind, detected_kind, signals FROM app.account_kind_review`, nil).Scan(&reviewOrg, &currentKind, &detected, &signals); err != nil {
		t.Fatalf("read review row: %v", err)
	}
	if reviewOrg != orgIDs["C"] || currentKind != "merchant" || detected != "ambiguous" {
		t.Errorf("review row: got org=%s current=%s detected=%s, want C/merchant/ambiguous", reviewOrg, currentKind, detected)
	}
	if !strings.Contains(signals, "handle") || !strings.Contains(signals, "business-kyc") {
		t.Errorf("review signals must name both halves, got %q", signals)
	}

	// Idempotency: re-applying up changes nothing.
	if _, err := pool.ExecEx(ctx, string(up057), nil); err != nil {
		t.Fatalf("re-apply 000057 up: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.account_kind_review`, nil).Scan(&reviewCount); err != nil || reviewCount != 1 {
		t.Errorf("re-apply must keep exactly 1 review row, got %d (err=%v)", reviewCount, err)
	}
	if kind, _ := kindOf(orgIDs["B"]); kind != "creator" {
		t.Errorf("re-apply must keep B creator, got %q", kind)
	}

	// Down removes scaffolding only — flipped kinds stay flipped (data).
	down057, err := os.ReadFile(filepath.Join(migDir, "000057_account_kind_backfill.down.sql"))
	if err != nil {
		t.Fatalf("read 000057 down: %v", err)
	}
	if _, err := pool.ExecEx(ctx, string(down057), nil); err != nil {
		t.Fatalf("apply 000057 down: %v", err)
	}
	var reviewTable sql.NullString
	if err := pool.QueryRowEx(ctx, `SELECT to_regclass('app.account_kind_review')::text`, nil).Scan(&reviewTable); err != nil {
		t.Fatalf("check review table dropped: %v", err)
	}
	if reviewTable.Valid {
		t.Errorf("review table should be dropped, got %q", reviewTable.String)
	}
	var hasSource bool
	if err := pool.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='app' AND table_name='organizations' AND column_name='account_kind_source')`, nil).Scan(&hasSource); err != nil {
		t.Fatalf("check source column dropped: %v", err)
	}
	if hasSource {
		t.Error("account_kind_source column should be dropped by down")
	}
	var kindB string
	if err := pool.QueryRowEx(ctx, `SELECT account_kind FROM app.organizations WHERE id = $1::uuid`, nil, orgIDs["B"]).Scan(&kindB); err != nil || kindB != "creator" {
		t.Errorf("down must preserve flipped kind (B stays creator), got %q (err=%v)", kindB, err)
	}
}

func mustParseConn(t *testing.T, url string) pgx.ConnConfig {
	t.Helper()
	cfg, err := pgx.ParseURI(url)
	if err != nil {
		t.Fatalf("parse db url: %v", err)
	}
	return cfg
}
