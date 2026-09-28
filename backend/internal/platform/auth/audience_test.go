package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"lipago/internal/platform/config"
)

func testVerifiers() (*Verifier, *Verifier) {
	customerCfg := config.AuthConfig{JWTSecret: "test-customer-secret-at-least-32-chars!!", TokenTTL: 24 * time.Hour}
	adminCfg := config.AuthConfig{
		JWTSecret:      "test-customer-secret-at-least-32-chars!!",
		AdminJWTSecret: "test-admin-secret-different-32-chars!!!!",
		AdminTokenTTL:  4 * time.Hour,
	}
	return NewCustomerVerifier(customerCfg, nil), NewAdminVerifier(adminCfg, nil)
}

// A customer token must never verify in the admin space and vice versa,
// even when both spaces share one signing key.
func TestAudiencePartitionRejectsCrossUse(t *testing.T) {
	customer, admin := testVerifiers()
	ctx := context.Background()

	customerToken, err := customer.IssueToken(User{ID: "user-1", Email: "m@example.com"})
	if err != nil {
		t.Fatalf("issue customer token: %v", err)
	}
	// Distinct signing keys: rejected at the signature check, before the
	// audience check is even reachable. Either way it is rejected — the
	// middleware's other-space probe turns this into invalid_audience.
	if _, err := admin.VerifyBearerToken(ctx, customerToken); err == nil {
		t.Fatal("admin verifier accepted customer token")
	}

	adminToken, err := admin.IssueToken(User{ID: "admin-1", Email: "op@example.com", IsAdmin: true})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	if _, err := customer.VerifyBearerToken(ctx, adminToken); err == nil {
		t.Fatal("customer verifier accepted admin token")
	}

	// Same audience still verifies in its own space.
	if _, err := customer.VerifyBearerToken(ctx, customerToken); err != nil {
		t.Fatalf("customer verifier rejected own token: %v", err)
	}
	if _, err := admin.VerifyBearerToken(ctx, adminToken); err != nil {
		t.Fatalf("admin verifier rejected own token: %v", err)
	}
}

// Pre-partition tokens carry no audience and must be rejected by both
// spaces (revoke-all on deploy: every session re-authenticates).
func TestLegacyTokenWithoutAudienceRejected(t *testing.T) {
	customer, admin := testVerifiers()
	ctx := context.Background()

	legacy := &Verifier{secret: []byte("test-customer-secret-at-least-32-chars!!"), tokenTTL: 24 * time.Hour}
	token, err := legacy.IssueToken(User{ID: "user-1", Email: "m@example.com"})
	if err != nil {
		t.Fatalf("issue legacy token: %v", err)
	}
	if _, err := customer.VerifyBearerToken(ctx, token); err == nil {
		t.Fatal("customer verifier accepted legacy token without audience")
	}
	if _, err := admin.VerifyBearerToken(ctx, token); err == nil {
		t.Fatal("admin verifier accepted legacy token without audience")
	}
}

// Audience separation holds even when both spaces share one secret.
func TestAudiencePartitionWithSharedSecret(t *testing.T) {
	cfg := config.AuthConfig{JWTSecret: "shared-secret-at-least-32-chars!!!!!!", TokenTTL: time.Hour}
	customer := NewCustomerVerifier(cfg, nil)
	admin := NewAdminVerifier(cfg, nil) // no AdminJWTSecret: falls back to shared
	ctx := context.Background()

	token, err := customer.IssueToken(User{ID: "user-1", Email: "m@example.com"})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	// Shared secret: the signature verifies, so the audience check is what
	// rejects — assert the specific error here.
	if _, err := admin.VerifyBearerToken(ctx, token); err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("admin verifier did not reject customer token on audience, err=%v", err)
	}
}
