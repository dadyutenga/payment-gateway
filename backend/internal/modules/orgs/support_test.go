package orgs

import (
	"context"
	"errors"
	"testing"
)

func TestReservedHandle(t *testing.T) {
	for _, h := range []string{"admin", "lipago", "support", "API", " SonicPesa ", "m-pesa", "checkout", "c", "kyc", "mpesa"} {
		if !ReservedHandle(h) {
			t.Errorf("expected handle %q reserved", h)
		}
	}
	for _, h := range []string{"amina.creates", "dj-dadi_01", "supporters", "administrator2", "lipago_fan"} {
		if ReservedHandle(h) {
			t.Errorf("expected handle %q allowed", h)
		}
	}
}

func TestCreateCreatorRejectsReservedHandle(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	_, vErrs, err := service.CreateCreatorOrganization(ctx, "fresh", "Page", CreatorOrgInput{DisplayName: "A", Handle: "admin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg := vErrs["handle"]; msg != "That handle is reserved — pick another." {
		t.Fatalf("expected reserved-handle message, got %v", vErrs)
	}
	if repo.createdOrgs != 0 {
		t.Fatal("reserved handle must not create an org")
	}
}

func TestUpdateOrganizationRejectsReservedHandle(t *testing.T) {
	repo := ownerRepo()
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator, Handle: "amina.creates", DisplayName: "Amina"}
	service := NewService(repo, nil)
	ctx := context.Background()

	// Unchanged handle is fine (no false positive on own handle).
	if _, vErrs, err := service.UpdateOrganization(ctx, "owner-1", "org_test", OrgProfileUpdate{Name: "Page", DisplayName: "Amina", Handle: "amina.creates"}); err != nil || vErrs.Any() {
		t.Fatalf("unchanged handle: err=%v errs=%v", err, vErrs)
	}
	// Switching to a reserved handle is refused.
	if _, vErrs, err := service.UpdateOrganization(ctx, "owner-1", "org_test", OrgProfileUpdate{Name: "Page", DisplayName: "Amina", Handle: "lipago"}); err != nil || vErrs["handle"] == "" {
		t.Fatalf("expected reserved-handle refusal, got err=%v errs=%v", err, vErrs)
	}
}

func creatorOrgRepo() *fakeOrgRepository {
	repo := ownerRepo()
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator, Handle: "amina.creates", DisplayName: "Amina"}
	return repo
}

func TestSaveSupportSettingsValidation(t *testing.T) {
	ctx := context.Background()

	// Merchant orgs are refused.
	merchant := ownerRepo()
	if _, vErrs, err := NewService(merchant, nil).SaveSupportSettings(ctx, "owner-1", "org_test", SupportSettingsInput{}); err != nil || !vErrs.Any() {
		t.Fatalf("expected merchant refused, got err=%v errs=%v", err, vErrs)
	}

	service := NewService(creatorOrgRepo(), nil)

	// Dust floor, inverted bounds, bad link mode each error.
	badModes := []SupportSettingsInput{
		{MinAmount: "100"},
		{MinAmount: "5000", MaxAmount: "1000"},
		{Links: []SupportLinkInput{{Label: "x", AmountMode: "whatever"}}},
		{Links: []SupportLinkInput{{Label: "", AmountMode: "open"}}},
		{Links: []SupportLinkInput{{Label: "Coffee", AmountMode: "fixed", Amount: ""}}},
	}
	for i, in := range badModes {
		if _, vErrs, err := service.SaveSupportSettings(ctx, "owner-1", "org_test", in); err != nil || !vErrs.Any() {
			t.Fatalf("case %d: expected validation errors, got err=%v errs=%v", i, err, vErrs)
		}
	}

	// Valid input persists with open-amount normalized amount-free.
	settings, vErrs, err := service.SaveSupportSettings(ctx, "owner-1", "org_test", SupportSettingsInput{
		SupportAppID: "app_test", MinAmount: "500", MaxAmount: "100000",
		Links: []SupportLinkInput{
			{Label: "Custom amount", AmountMode: "open", Amount: "999"},
			{Label: "Buy me coffee", AmountMode: "fixed", Amount: "5000"},
		},
	})
	if err != nil || vErrs.Any() {
		t.Fatalf("save settings: err=%v errs=%v", err, vErrs)
	}
	if settings.SupportAppID != "app_test" || len(settings.Links) != 2 {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	if settings.Links[0].Amount != "" {
		t.Fatalf("open link must not store an amount: %+v", settings.Links[0])
	}
}

func TestPublicCreatorSupport(t *testing.T) {
	ctx := context.Background()
	repo := creatorOrgRepo()
	repo.support = &SupportSettings{
		OrgID: "org_test", SupportAppID: "app_test",
		Links: []SupportLink{{ID: "l1", OrgID: "org_test", Label: "Coffee", AmountMode: "fixed", Amount: "5000", Active: true}},
	}
	repo.survey = &CreatorSurvey{OrgID: "org_test", Category: CreatorCategoryContentCreator}
	service := NewService(repo, nil)

	data, err := service.PublicCreatorSupport(ctx, "amina.creates")
	if err != nil {
		t.Fatalf("public support: %v", err)
	}
	if !data.Enabled || data.Category != CreatorCategoryContentCreator || len(data.Settings.Links) != 1 {
		t.Fatalf("unexpected page data: %+v", data)
	}

	// No settings row: profile resolves, page disabled.
	bare := NewService(creatorOrgRepo(), nil)
	data, err = bare.PublicCreatorSupport(ctx, "amina.creates")
	if err != nil || data.Enabled {
		t.Fatalf("expected disabled page, got %+v err=%v", data, err)
	}
}

func TestEnsureSupportPageSeedsDefaults(t *testing.T) {
	ctx := context.Background()
	repo := creatorOrgRepo()
	service := NewService(repo, nil)

	settings, err := service.EnsureSupportPage(ctx, "owner-1", "org_test", "app_new")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if settings.SupportAppID != "app_new" || len(settings.Links) != 2 {
		t.Fatalf("expected linked app + 2 seeded buttons, got %+v", settings)
	}

	// Already linked: returns as-is, no reseed.
	again, err := service.EnsureSupportPage(ctx, "owner-1", "org_test", "app_other")
	if err != nil || again.SupportAppID != "app_new" {
		t.Fatalf("expected existing link kept, got %+v err=%v", again, err)
	}

	// Merchant orgs cannot enable.
	if _, err := NewService(ownerRepo(), nil).EnsureSupportPage(ctx, "owner-1", "org_test", "app_new"); !errors.Is(err, ErrSupportPageDisabled) {
		t.Fatalf("expected ErrSupportPageDisabled, got %v", err)
	}
}

func TestRequireCreatorOrg(t *testing.T) {
	ctx := context.Background()
	service := NewService(creatorOrgRepo(), nil)
	if _, err := service.RequireCreatorOrg(ctx, "owner-1", "org_test", PermRead); err != nil {
		t.Fatalf("creator require: %v", err)
	}
	if _, err := NewService(ownerRepo(), nil).RequireCreatorOrg(ctx, "owner-1", "org_test", PermRead); !errors.Is(err, ErrNotCreatorOrg) {
		t.Fatalf("expected ErrNotCreatorOrg, got %v", err)
	}
}
