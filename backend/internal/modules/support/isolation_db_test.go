package support

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"lipago/internal/platform/auth"
)

// Exercises the customer response path against PostgreSQL, including both
// tenant isolation and query-level exclusion of admin-only internal notes.
func TestCustomerTicketIsolationAndInternalNotes(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — needs postgres with support migrations")
	}
	ctx := context.Background()
	connCfg, err := pgx.ParseConnectionString(dbURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: connCfg, MaxConnections: 3, AcquireTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	suffix := uuid.NewString()
	emailA, emailB := "support-a-"+suffix+"@example.com", "support-b-"+suffix+"@example.com"
	var userA, userB, orgID, foreignOrgID, ticketID, messageID string
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.users (email, password_hash) VALUES ($1, 'x') RETURNING id::text`, nil, emailA).Scan(&userA); err != nil {
		t.Fatalf("insert user A: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.users (email, password_hash) VALUES ($1, 'x') RETURNING id::text`, nil, emailB).Scan(&userB); err != nil {
		t.Fatalf("insert user B: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug) VALUES ($1, $2) RETURNING id::text`, nil, "support-a-"+suffix, "support-a-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization A: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.organizations (name, slug) VALUES ($1, $2) RETURNING id::text`, nil, "support-b-"+suffix, "support-b-"+suffix).Scan(&foreignOrgID); err != nil {
		t.Fatalf("insert organization B: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `INSERT INTO app.org_members (org_id, user_id, role, status) VALUES ($1::uuid, $2::uuid, 'owner', 'active')`, nil, orgID, userA); err != nil {
		t.Fatalf("insert membership A: %v", err)
	}
	if _, err := pool.ExecEx(ctx, `INSERT INTO app.org_members (org_id, user_id, role, status) VALUES ($1::uuid, $2::uuid, 'owner', 'active')`, nil, foreignOrgID, userB); err != nil {
		t.Fatalf("insert membership B: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.support_tickets (org_id, created_by, subject, category) VALUES ($1::uuid, $2::uuid, 'isolation test', 'other') RETURNING id::text`, nil, orgID, userA).Scan(&ticketID); err != nil {
		t.Fatalf("insert ticket: %v", err)
	}
	if err := pool.QueryRowEx(ctx, `INSERT INTO app.support_messages (ticket_id, author_type, author_id, body, internal_note) VALUES ($1::uuid, 'admin', $2::uuid, 'private staff note', TRUE) RETURNING id::text`, nil, ticketID, uuid.NewString()).Scan(&messageID); err != nil {
		t.Fatalf("insert internal note: %v", err)
	}
	defer func() {
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.organizations WHERE id IN ($1::uuid, $2::uuid)`, nil, orgID, foreignOrgID)
		_, _ = pool.ExecEx(ctx, `DELETE FROM app.users WHERE id IN ($1::uuid, $2::uuid)`, nil, userA, userB)
	}()

	handler := NewHandler(NewService(pool, nil, nil, time.Hour))
	request := func(userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/merchant/orgs/ignored/support/tickets/"+ticketID, nil)
		req.SetPathValue("ticketID", ticketID)
		req = req.WithContext(auth.WithClaims(req.Context(), auth.Claims{Subject: userID, Audience: auth.AudienceCustomer}))
		rec := httptest.NewRecorder()
		handler.CustomerGet(rec, req)
		return rec
	}
	ownerResponse := request(userA)
	if ownerResponse.Code != 200 {
		t.Fatalf("ticket owner response status = %d, want 200: %s", ownerResponse.Code, ownerResponse.Body.String())
	}
	if strings.Contains(ownerResponse.Body.String(), "private staff note") || strings.Contains(ownerResponse.Body.String(), messageID) {
		t.Fatalf("customer response leaked an internal note: %s", ownerResponse.Body.String())
	}
	foreignResponse := request(userB)
	if foreignResponse.Code != 404 {
		t.Fatalf("foreign tenant response status = %d, want 404: %s", foreignResponse.Code, foreignResponse.Body.String())
	}
	if strings.Contains(foreignResponse.Body.String(), ticketID) {
		t.Fatalf("foreign tenant response leaked ticket ID: %s", foreignResponse.Body.String())
	}
}
