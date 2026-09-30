package payments

import (
	"context"
	"testing"
	"time"

	"lipago/internal/modules/orgs"
	"lipago/internal/modules/payments/provider"
)

func supportTestService(repo *fakePaymentRepository) *Service {
	return NewService(repo, nil, testCipher, testServiceOptions(), nil)
}

func TestSupportBoundsFloorsAndClamps(t *testing.T) {	repo := &fakePaymentRepository{}
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

func TestCreatorTierSitsBelowMerchant(t *testing.T) {	ctx := context.Background()
	app := PaymentApp{ID: "app_test", OrgID: "org_test"}

	merchant := supportTestService(&fakePaymentRepository{})
	min, max, err := merchant.SupportBounds(ctx, app, "", "")
	if err != nil || min != "500" || max != "5000000" {
		t.Fatalf("merchant bounds = (%q, %q), want (500, 5000000) (err=%v)", min, max, err)
	}

	creator := supportTestService(&fakePaymentRepository{orgAccountKind: "creator"})
	min, max, err = creator.SupportBounds(ctx, app, "", "")
	if err != nil || min != "500" || max != "1000000" {
		t.Fatalf("creator bounds = (%q, %q), want (500, 1000000) (err=%v)", min, max, err)
	}

	// Admin override still wins for creators (manual raise path).
	raised := supportTestService(&fakePaymentRepository{orgAccountKind: "creator", orgLiveMaxTxn: "3000000"})
	_, max, err = raised.SupportBounds(ctx, app, "", "")
	if err != nil || max != "3000000" {
		t.Fatalf("raised creator max = %q, want 3000000 (err=%v)", max, err)
	}
}

func TestMatchIdentityName(t *testing.T) {
	if !matchIdentityName("Amina Juma", "Amina Juma") {
		t.Error("exact match must pass")
	}
	if !matchIdentityName("juma amina", "Amina Juma") {
		t.Error("order-insensitive match must pass")
	}
	if !matchIdentityName("Amina", "Amina Juma") {
		t.Error("subset match must pass")
	}
	if matchIdentityName("Amina Juma", "Baraka Moyo") {
		t.Error("different names must fail")
	}
	if matchIdentityName("", "Amina Juma") || matchIdentityName("Amina", "") {
		t.Error("empty names must fail")
	}
}

func TestSaveCreatorPayoutDestination(t *testing.T) {
	ctx := context.Background()
	repo := &fakePaymentRepository{orgAccountKind: "creator"}
	service := supportTestService(repo)

	// Bad phone + missing name → field errors, nothing stored.
	_, vErrs, err := service.SaveCreatorPayoutDestination(ctx, "org_test", "Amina Juma", SavePayoutDestinationInput{
		Provider: "mpesa", Phone: "0712345678", AccountName: "",
	})
	if err != nil || !vErrs.Any() {
		t.Fatalf("expected validation errors, got err=%v errs=%v", err, vErrs)
	}
	if _, ok := vErrs["phone"]; !ok {
		t.Errorf("expected phone error, got %v", vErrs)
	}

	// Attested name must match the verified identity.
	_, vErrs, err = service.SaveCreatorPayoutDestination(ctx, "org_test", "Amina Juma", SavePayoutDestinationInput{
		Provider: "mpesa", Phone: "+255712345678", AccountName: "Baraka Moyo",
	})
	if err != nil || vErrs["account_name"] == "" {
		t.Fatalf("expected identity-mismatch error, got err=%v errs=%v", err, vErrs)
	}

	// Unverified identity blocks with a sentinel, not a field error.
	if _, _, err := service.SaveCreatorPayoutDestination(ctx, "org_test", "", SavePayoutDestinationInput{
		Provider: "mpesa", Phone: "+255712345678", AccountName: "Amina Juma",
	}); err != ErrCreatorIdentityUnverified {
		t.Fatalf("expected ErrCreatorIdentityUnverified, got %v", err)
	}

	// Merchant orgs are refused.
	merchant := supportTestService(&fakePaymentRepository{})
	if _, _, err := merchant.SaveCreatorPayoutDestination(ctx, "org_test", "Acme", SavePayoutDestinationInput{
		Provider: "mpesa", Phone: "+255712345678", AccountName: "Acme",
	}); err == nil {
		t.Fatal("expected merchant org refused")
	}

	// First save: effective immediately, provider lookup unavailable (noted).
	dest, vErrs, err := service.SaveCreatorPayoutDestination(ctx, "org_test", "Amina Juma", SavePayoutDestinationInput{
		Provider: "mpesa", Phone: "+255712345678", AccountName: "Amina Juma",
	})
	if err != nil || vErrs.Any() {
		t.Fatalf("save: err=%v errs=%v", err, vErrs)
	}
	if dest.NameMatch != "unavailable" || dest.NameMatchDetail == "" {
		t.Fatalf("provider lookup must be noted unavailable, got %+v", dest)
	}
	if time.Now().UTC().Before(dest.EffectiveAt.Add(-time.Minute)) {
		t.Fatalf("first save must be effective immediately, got %v", dest.EffectiveAt)
	}

	// Changing the number starts the 24h cooling period.
	changed, _, err := service.SaveCreatorPayoutDestination(ctx, "org_test", "Amina Juma", SavePayoutDestinationInput{
		Provider: "tigo", Phone: "+255677777777", AccountName: "Amina Juma",
	})
	if err != nil {
		t.Fatalf("change: %v", err)
	}
	if time.Until(changed.EffectiveAt) < 23*time.Hour {
		t.Fatalf("changed destination must cool ~24h, got %v", changed.EffectiveAt)
	}

	// Identical re-save stays effective immediately.
	same, _, err := service.SaveCreatorPayoutDestination(ctx, "org_test", "Amina Juma", SavePayoutDestinationInput{
		Provider: "tigo", Phone: "+255677777777", AccountName: "Amina Juma",
	})
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if time.Now().UTC().Before(same.EffectiveAt.Add(-time.Minute)) {
		t.Fatalf("identical re-save must stay effective, got %v", same.EffectiveAt)
	}
}

func TestCreatorWithdrawalDestinationEnforcement(t *testing.T) {
	ctx := context.Background()
	newCreatorService := func(dest *CreatorPayoutDestination) *Service {
		return supportTestService(&fakePaymentRepository{orgAccountKind: "creator", payoutDestination: dest})
	}
	withdraw := func(s *Service) (map[string]string, error) {
		_, vErrs, err := s.CreateWithdrawal(ctx, "user-1", CreateWithdrawalInput{
			AppID: "app_test", Amount: "1000", Currency: "TZS",
			DestinationType: "mobile_money",
			DestinationDetails: map[string]any{"phone": "+255712345678"},
		})
		return vErrs, err
	}

	// No saved destination → field error directing to save first.
	if vErrs, err := withdraw(newCreatorService(nil)); err != nil || vErrs["destination"] == "" {
		t.Fatalf("expected save-first error, got err=%v errs=%v", err, vErrs)
	}

	// Wrong number → mismatch sentinel.
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := withdraw(newCreatorService(&CreatorPayoutDestination{Phone: "+255699999999", EffectiveAt: past})); err != ErrWithdrawalDestinationMismatch {
		t.Fatalf("expected mismatch, got %v", err)
	}

	// Right number but cooling → cooling sentinel.
	future := time.Now().UTC().Add(time.Hour)
	if _, err := withdraw(newCreatorService(&CreatorPayoutDestination{Phone: "+255712345678", EffectiveAt: future})); err != ErrWithdrawalDestinationCooling {
		t.Fatalf("expected cooling, got %v", err)
	}

	// Merchant orgs pass through the destination checks untouched (the
	// fake balance is zero, so insufficient balance — never a
	// destination error — is the only acceptable outcome here).
	merchant := supportTestService(&fakePaymentRepository{})
	if _, _, err := merchant.CreateWithdrawal(ctx, "user-1", CreateWithdrawalInput{
		AppID: "app_test", Amount: "1000", Currency: "TZS",
		DestinationType: "bank", DestinationDetails: map[string]any{"account_number": "123"},
	}); err != ErrInsufficientBalance {
		t.Fatalf("merchant withdrawal must skip destination checks, got %v", err)
	}
}
