package payments

import (
	"context"
	"testing"

	"lipago/internal/modules/orgs"
	"lipago/internal/modules/payments/provider"
)

func supportTestService(repo *fakePaymentRepository) *Service {
	return NewService(repo, nil, testCipher, testServiceOptions(), nil)
}

func TestSupportBoundsFloorsAndClamps(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := supportTestService(repo)
	ctx := context.Background()
	app := PaymentApp{ID: "app_test", OrgID: "org_test"}

	// No config: 500 floor, platform live-max ceiling.
	min, max, err := service.SupportBounds(ctx, app, "", "")
	if err != nil {
		t.Fatalf("bounds: %v", err)
	}
	if min != "500" || max != "5000000" {
		t.Fatalf("default bounds = (%q, %q), want (500, 5000000)", min, max)
	}

	// Config below the dust floor is lifted to 500, never below.
	min, _, err = service.SupportBounds(ctx, app, "100", "")
	if err != nil || min != "500" {
		t.Fatalf("sub-floor min = %q, want 500 (err=%v)", min, err)
	}

	// Config above the live tier is clamped down — self-reported values
	// can never raise limits.
	min, max, err = service.SupportBounds(ctx, app, "1000", "999999999")
	if err != nil || min != "1000" || max != "5000000" {
		t.Fatalf("clamped bounds = (%q, %q), want (1000, 5000000) (err=%v)", min, max, err)
	}

	// Narrower config is honored.
	_, max, err = service.SupportBounds(ctx, app, "", "100000")
	if err != nil || max != "100000" {
		t.Fatalf("narrow max = %q, want 100000 (err=%v)", max, err)
	}

	// Org override wins over the platform default as the ceiling source.
	repo.orgLiveMaxTxn = "2000000"
	_, max, err = service.SupportBounds(ctx, app, "", "")
	if err != nil || max != "2000000" {
		t.Fatalf("org ceiling = %q, want 2000000 (err=%v)", max, err)
	}
}

func TestResolveSupportApp(t *testing.T) {
	repo := &fakePaymentRepository{
		appsByOrg: []PaymentApp{{ID: "app_support", OrgID: "org_test", Status: "active"}},
	}
	service := supportTestService(repo)
	ctx := context.Background()

	app, err := service.ResolveSupportApp(ctx, "org_test", "app_support")
	if err != nil || app.ID != "app_support" {
		t.Fatalf("resolve linked app: %+v err=%v", app, err)
	}
	if _, err := service.ResolveSupportApp(ctx, "org_test", "app_other"); err == nil {
		t.Fatal("expected foreign/missing app rejected")
	} else if err != orgs.ErrSupportPageDisabled {
		t.Fatalf("expected ErrSupportPageDisabled, got %v", err)
	}
}

func TestUsableProviderKinds(t *testing.T) {
	repo := &fakePaymentRepository{
		providerAccountList: []PaymentProviderAccount{
			{Provider: "sonicpesa", IsDefault: true, Status: "active"},
			{Provider: "azampesa", IsDefault: true, Status: "suspended"},
			{Provider: "ghost", IsDefault: true, Status: "active"},
			{Provider: "sonicpesa", IsDefault: false, Status: "active"},
		},
	}
	// Registry with only sonicpesa: unregistered kinds ("ghost") and
	// inactive accounts are excluded even when marked default.
	registry := map[string]provider.Constructor{
		"sonicpesa": func(_ string, _ map[string]string) provider.PaymentProvider { return nil },
	}
	service := NewService(repo, registry, testCipher, testServiceOptions(), nil)
	kinds, err := service.UsableProviderKinds(context.Background())
	if err != nil {
		t.Fatalf("usable kinds: %v", err)
	}
	if len(kinds) != 1 || kinds[0] != "sonicpesa" {
		t.Fatalf("usable kinds = %v, want [sonicpesa]", kinds)
	}
}
