package analytics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx"

	"lipago/internal/modules/orgs"
	"lipago/internal/platform/auth"
	"lipago/internal/platform/config"
	"lipago/internal/platform/middleware"
)

// TestAnalyticsIsolationAndPartition runs the real middleware + handler +
// DB chain over HTTP: cross-org reads must 403 on every merchant
// endpoint, customer tokens must 401 on admin routes and vice versa.
// Requires DATABASE_URL; skipped otherwise.
func TestAnalyticsIsolationAndPartition(t *testing.T) {
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

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	custEmail := "iso-cust-" + suffix + "@example.com"
	adminEmail := "iso-admin-" + suffix + "@example.com"
	var custID, adminID, orgA, orgB, appB string
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.users (email, password_hash) VALUES ($1, 'x') RETURNING id::text`,
		nil, custEmail).Scan(&custID); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.admin_users (email, password_hash) VALUES ($1, 'x') RETURNING id::text`,
		nil, adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		nil, "iso-a-"+suffix, "iso-a-"+suffix).Scan(&orgA); err != nil {
		t.Fatalf("insert orgA: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		nil, "iso-b-"+suffix, "iso-b-"+suffix).Scan(&orgB); err != nil {
		t.Fatalf("insert orgB: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `INSERT INTO app.org_members (org_id, user_id, role, status) VALUES ($1::uuid, $2::uuid, 'owner', 'active')`,
		nil, orgA, custID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.payment_apps (name, org_id) VALUES ($1, $2::uuid) RETURNING id::text`,
		nil, "iso-app-b-"+suffix, orgB).Scan(&appB); err != nil {
		t.Fatalf("insert appB: %v", err)
	}
	defer func() {
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.payment_apps WHERE id = $1::uuid`, nil, appB)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.org_members WHERE user_id = $1::uuid`, nil, custID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE id IN ($1::uuid, $2::uuid)`, nil, orgA, orgB)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.users WHERE id = $1::uuid`, nil, custID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.admin_users WHERE id = $1::uuid`, nil, adminID)
	}()

	authCfg := config.AuthConfig{
		JWTSecret:      "isolation-test-customer-secret-32chars!",
		AdminJWTSecret: "isolation-test-admin-secret-32chars!!!",
		TokenTTL:       time.Hour,
		AdminTokenTTL:  time.Hour,
	}
	customerVerifier := auth.NewCustomerVerifier(authCfg, nil)
	adminVerifier := auth.NewAdminVerifier(authCfg, nil)
	authSvc := auth.NewService(pool, customerVerifier)
	custToken, err := customerVerifier.IssueToken(auth.User{ID: custID, Email: custEmail})
	if err != nil {
		t.Fatalf("issue customer token: %v", err)
	}
	adminToken, err := adminVerifier.IssueToken(auth.User{ID: adminID, Email: adminEmail, IsAdmin: true})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	orgsSvc := orgs.NewService(orgs.NewPostgresRepository(pool), nil)
	svc := NewService(NewPostgresRepository(pool), nil)
	h := NewHandler(svc, nil)
	h.SetOrgsService(orgsSvc)

	customerChain := func(next http.HandlerFunc) http.Handler {
		return middleware.Chain(next,
			middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authSvc, false),
			middleware.RateLimit(1000))
	}
	adminChain := func(next http.HandlerFunc) http.Handler {
		return middleware.Chain(next,
			middleware.RequireAdminAuth(adminVerifier, customerVerifier, authSvc),
			middleware.RateLimit(1000))
	}
	call := func(chain http.Handler, token, orgID string, extra map[string]string) *httptest.ResponseRecorder {
		// Route shape mirrors app.go (orgID path value + optional query).
		req := httptest.NewRequest("GET", "/x", nil)
		req.SetPathValue("orgID", orgID)
		q := req.URL.Query()
		for k, v := range extra {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec
	}

	merchantEndpoints := map[string]http.HandlerFunc{
		"overview":  h.MerchantOverview,
		"methods":   h.MerchantMethods,
		"peak":      h.MerchantPeakHours,
		"customers": h.MerchantCustomers,
		"failures":  h.MerchantFailures,
		"apps":      h.MerchantApps,
		"settle":    h.MerchantSettlements,
	}
	for name, fn := range merchantEndpoints {
		// Own org: allowed (200, possibly empty data).
		if rec := call(customerChain(fn), custToken, orgA, nil); rec.Code != 200 {
			t.Fatalf("%s own org: want 200, got %d (%s)", name, rec.Code, rec.Body.String())
		}
		// Other org: denied.
		if rec := call(customerChain(fn), custToken, orgB, nil); rec.Code != 403 {
			t.Fatalf("%s cross-org: want 403, got %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
	// Foreign app_id smuggled into own org: denied.
	if rec := call(customerChain(h.MerchantOverview), custToken, orgA, map[string]string{"app_id": appB}); rec.Code != 403 {
		t.Fatalf("foreign app_id: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Partition: customer token on admin route.
	adminOverview := adminChain(h.Overview)
	req := httptest.NewRequest("GET", "/admin/analytics/overview", nil)
	req.Header.Set("Authorization", "Bearer "+custToken)
	rec := httptest.NewRecorder()
	adminOverview.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "invalid_audience") {
		t.Fatalf("customer on admin route: want 401 invalid_audience, got %d (%s)", rec.Code, rec.Body.String())
	}
	// Admin token on customer route.
	if rec := call(customerChain(h.MerchantOverview), adminToken, orgA, nil); rec.Code != 401 ||
		!strings.Contains(rec.Body.String(), "invalid_audience") {
		t.Fatalf("admin on customer route: want 401 invalid_audience, got %d (%s)", rec.Code, rec.Body.String())
	}
	// Admin token on admin route: allowed.
	areq := httptest.NewRequest("GET", "/admin/analytics/overview", nil)
	areq.Header.Set("Authorization", "Bearer "+adminToken)
	arec := httptest.NewRecorder()
	adminOverview.ServeHTTP(arec, areq)
	if arec.Code != 200 {
		t.Fatalf("admin on admin route: want 200, got %d (%s)", arec.Code, arec.Body.String())
	}
}
