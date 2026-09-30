package orgs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidHandle(t *testing.T) {
	valid := []string{"amina.creates", "dj-dadi_01", "abc"}
	invalid := []string{"", "ab", "UPPER", "has space", "toolonghandletoolonghandletoolong", "bad!"}
	for _, h := range valid {
		if !ValidHandle(h) {
			t.Errorf("expected handle %q valid", h)
		}
	}
	for _, h := range invalid {
		if ValidHandle(h) {
			t.Errorf("expected handle %q invalid", h)
		}
	}
}

func TestParseAccountKind(t *testing.T) {
	if k, _ := ParseAccountKind(""); k != AccountKindMerchant {
		t.Fatalf("empty kind should default merchant, got %q", k)
	}
	if k, _ := ParseAccountKind("creator"); k != AccountKindCreator {
		t.Fatalf("creator kind, got %q", k)
	}
	if _, err := ParseAccountKind("enterprise"); err == nil {
		t.Fatal("expected unknown kind rejected")
	}
}

func TestCreateCreatorRequiresHandle(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	// owner-1 already holds org_test → blocked by single-org rule first.
	if _, _, err := service.CreateCreatorOrganization(ctx, "owner-1", "Page", CreatorOrgInput{DisplayName: "A", Handle: "a-valid-handle"}); err == nil {
		t.Fatal("expected second org blocked")
	}
	// Fresh user, bad handle → validation errors.
	if _, vErrs, err := service.CreateCreatorOrganization(ctx, "fresh", "Page", CreatorOrgInput{DisplayName: "Amina", Handle: "AB"}); err != nil || !vErrs.Any() {
		t.Fatalf("expected handle validation errors, got err=%v errs=%v", err, vErrs)
	}
	// Fresh user, good input → success.
	org, vErrs, err := service.CreateCreatorOrganization(ctx, "fresh", "Page", CreatorOrgInput{DisplayName: "Amina Creates", Handle: "amina.creates", Bio: "Videos"})
	if err != nil || vErrs.Any() {
		t.Fatalf("expected creator create success, got err=%v errs=%v", err, vErrs)
	}
	if org.AccountKind != AccountKindCreator || org.Handle != "amina.creates" {
		t.Fatalf("unexpected creator org: %+v", org)
	}
}

func TestKYCIsKindBranched(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	// Merchant org rejects creator evidence path via business endpoint? No —
	// business endpoint on merchant org works (baseline).
	if _, vErrs, err := service.SubmitKYC(ctx, "owner-1", "org_test", "Acme", "123456789", "uploads/kyc/d.pdf"); err != nil || vErrs.Any() {
		t.Fatalf("merchant submit baseline: err=%v errs=%v", err, vErrs)
	}

	// Flip fixture org to creator: business KYC must refuse explicitly.
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator}
	if _, vErrs, err := service.SubmitKYC(ctx, "owner-1", "org_test", "Acme", "123456789", "uploads/kyc/d.pdf"); err != nil || !vErrs.Any() {
		t.Fatalf("expected creator refused on business endpoint, got err=%v errs=%v", err, vErrs)
	}
	// Creator endpoint validates individual fields.
	_, vErrs, err := service.SubmitCreatorKYC(ctx, "owner-1", "org_test", CreatorKYCInput{})
	if err != nil || !vErrs.Any() {
		t.Fatalf("expected creator validation errors, got err=%v errs=%v", err, vErrs)
	}
	for _, field := range []string{"full_name", "id_type", "id_number", "dob", "id_document_url", "selfie_url"} {
		if _, ok := vErrs[field]; !ok {
			t.Errorf("expected error on %s", field)
		}
	}
	sub, vErrs, err := service.SubmitCreatorKYC(ctx, "owner-1", "org_test", CreatorKYCInput{FullName: "Amina Juma", IDType: "national_id", IDNumber: "19900101-00001-00001-00", Dob: "1990-01-01", DocURL: "uploads/kyc/d.pdf", SelfieURL: "uploads/kyc/s.pdf"})
	if err != nil || vErrs.Any() {
		t.Fatalf("creator submit: err=%v errs=%v", err, vErrs)
	}
	if sub.FullName != "Amina Juma" {
		t.Fatalf("unexpected creator submission: %+v", sub)
	}
}

func TestValidCreatorDOB(t *testing.T) {
	now := time.Now().UTC()
	eighteen := now.AddDate(-18, 0, 0)
	adult := eighteen.AddDate(-1, 0, 0).Format("2006-01-02")
	almostAdult := eighteen.AddDate(0, 0, 1).Format("2006-01-02")
	for _, dob := range []string{adult, "1990-01-01", "1985-12-31", eighteen.Format("2006-01-02")} {
		if !ValidCreatorDOB(dob) {
			t.Errorf("expected DOB %q valid (18+)", dob)
		}
	}
	for _, dob := range []string{"", "not-a-date", "01-01-1990", "3000-01-01", almostAdult, now.Format("2006-01-02"), "1899-12-31"} {
		if ValidCreatorDOB(dob) {
			t.Errorf("expected DOB %q invalid", dob)
		}
	}
}

func TestCreatorKYCBlocksUnder18WithoutStoring(t *testing.T) {
	repo := ownerRepo()
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator}
	service := NewService(repo, nil)
	ctx := context.Background()

	under18 := time.Now().UTC().AddDate(-10, 0, 0).Format("2006-01-02")
	in := CreatorKYCInput{FullName: "Young Creator", IDType: "passport", IDNumber: "P123", Dob: under18, DocURL: "uploads/kyc/d.pdf", SelfieURL: "uploads/kyc/s.jpg"}
	_, vErrs, err := service.SubmitCreatorKYC(ctx, "owner-1", "org_test", in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg, ok := vErrs["dob"]
	if !ok || msg == "" {
		t.Fatalf("expected clear 18+ message on dob, got %v", vErrs)
	}
	// Nothing persisted: the fake repo records no submission and the org
	// never leaves pending (SubmitCreatorKYC returns before any repo call
	// on validation failure — verified by the fake returning zero values).
	if _, found, _ := repo.GetKYCSubmission(ctx, "org_test"); found {
		t.Fatal("under-18 submission must not be stored")
	}

	// Missing selfie is also a validation refusal, not a stored rejection.
	adult := in
	adult.Dob = "2000-05-05"
	adult.SelfieURL = ""
	if _, vErrs, err := service.SubmitCreatorKYC(ctx, "owner-1", "org_test", adult); err != nil || vErrs["selfie_url"] == "" {
		t.Fatalf("expected selfie_url validation error, got err=%v errs=%v", err, vErrs)
	}
}

func TestSuggestedCreatorRiskTier(t *testing.T) {
	cases := []struct{ volume, txn, want string }{
		{CreatorVolumeUnder100K, CreatorTxnUnder50, CreatorRiskStandard},
		{CreatorVolume100KTo1M, CreatorTxn50To200, CreatorRiskStandard},
		{CreatorVolume1MTo10M, CreatorTxnUnder50, CreatorRiskElevated},
		{CreatorVolume100KTo1M, CreatorTxn200To1000, CreatorRiskElevated},
		{CreatorVolume10MTo100M, CreatorTxnUnder50, CreatorRiskHigh},
		{CreatorVolumeOver100M, CreatorTxnUnder50, CreatorRiskHigh},
		{CreatorVolumeUnder100K, CreatorTxnOver5000, CreatorRiskHigh},
		{"", "", CreatorRiskStandard},
	}
	for _, tc := range cases {
		if got := SuggestedCreatorRiskTier(tc.volume, tc.txn); got != tc.want {
			t.Errorf("SuggestedCreatorRiskTier(%q, %q) = %q, want %q", tc.volume, tc.txn, got, tc.want)
		}
	}
}

func validSurveyInput() CreatorSurveyInput {
	return CreatorSurveyInput{
		DisplayName:        "Amina Creates",
		Category:           CreatorCategoryContentCreator,
		ReferralSource:     CreatorReferralSocialMedia,
		UseCases:           []string{CreatorUseSupportTips},
		ExpectedVolumeBand: CreatorVolume100KTo1M,
		ExpectedTxnBand:    CreatorTxn50To200,
	}
}

func TestSaveCreatorSurveyValidation(t *testing.T) {
	repo := ownerRepo()
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator}
	service := NewService(repo, nil)
	ctx := context.Background()

	// Merchant org refused.
	repoMerchant := ownerRepo()
	repoMerchant.org = Organization{ID: "org_test", Name: "Org", KYCStatus: "pending", AccountKind: AccountKindMerchant}
	svcMerchant := NewService(repoMerchant, nil)
	if _, vErrs, err := svcMerchant.SaveCreatorSurvey(ctx, "owner-1", "org_test", validSurveyInput()); err != nil || !vErrs.Any() {
		t.Fatalf("expected merchant refused, got err=%v errs=%v", err, vErrs)
	}

	// Non-member blocked.
	if _, _, err := service.SaveCreatorSurvey(ctx, "stranger", "org_test", validSurveyInput()); err == nil {
		t.Fatal("expected non-member blocked")
	}

	// Empty use cases + bad bands + Other without text → errors on each.
	bad := validSurveyInput()
	bad.UseCases = nil
	bad.ExpectedVolumeBand = "millions"
	bad.ExpectedTxnBand = "lots"
	bad.Category = CreatorCategoryOther
	bad.CategoryOther = ""
	if _, vErrs, err := service.SaveCreatorSurvey(ctx, "owner-1", "org_test", bad); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else {
		for _, field := range []string{"use_cases", "expected_volume_band", "expected_txn_band", "category_other"} {
			if _, ok := vErrs[field]; !ok {
				t.Errorf("expected error on %s", field)
			}
		}
	}

	// Valid input succeeds with derived tier.
	in := validSurveyInput()
	survey, vErrs, err := service.SaveCreatorSurvey(ctx, "owner-1", "org_test", in)
	if err != nil || vErrs.Any() {
		t.Fatalf("save survey: err=%v errs=%v", err, vErrs)
	}
	if survey.SuggestedRiskTier != CreatorRiskStandard {
		t.Fatalf("unexpected tier: %+v", survey)
	}
	hi := validSurveyInput()
	hi.ExpectedVolumeBand = CreatorVolumeOver100M
	hiSurvey, _, err := service.SaveCreatorSurvey(ctx, "owner-1", "org_test", hi)
	if err != nil || hiSurvey.SuggestedRiskTier != CreatorRiskHigh {
		t.Fatalf("expected high tier, got %+v err=%v", hiSurvey, err)
	}
}

func TestGetCreatorSurveyNotFound(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()
	if _, err := service.GetCreatorSurvey(ctx, "owner-1", "org_test"); !errors.Is(err, ErrSurveyNotFound) {
		t.Fatalf("expected ErrSurveyNotFound, got %v", err)
	}
	repo.survey = &CreatorSurvey{OrgID: "org_test", Category: CreatorCategoryCoachEducator, SuggestedRiskTier: CreatorRiskStandard}
	if _, err := service.GetCreatorSurvey(ctx, "owner-1", "org_test"); err != nil {
		t.Fatalf("expected survey found, got %v", err)
	}
}

func TestUpdateOrganizationBranchesByKind(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	// Creator org: business fields rejected, creator fields accepted.
	repo.org = Organization{ID: "org_test", Name: "Page", KYCStatus: "pending", AccountKind: AccountKindCreator, Handle: "amina.creates", DisplayName: "Amina"}
	if _, vErrs, _ := service.UpdateOrganization(ctx, "owner-1", "org_test", OrgProfileUpdate{Name: "Page", BusinessName: "Acme", DisplayName: "Amina", Handle: "amina.creates"}); !vErrs.Any() {
		t.Fatal("expected business fields rejected on creator org")
	}
	if _, vErrs, err := service.UpdateOrganization(ctx, "owner-1", "org_test", OrgProfileUpdate{Name: "Page", DisplayName: "Amina Creates", Handle: "amina.creates", Bio: "Hi"}); err != nil || vErrs.Any() {
		t.Fatalf("creator update: err=%v errs=%v", err, vErrs)
	}
	// Merchant org: creator fields rejected.
	repo.org = Organization{ID: "org_test", Name: "Org", KYCStatus: "pending", AccountKind: AccountKindMerchant}
	if _, vErrs, _ := service.UpdateOrganization(ctx, "owner-1", "org_test", OrgProfileUpdate{Name: "Org", DisplayName: "X", Handle: "x-handle-ok"}); !vErrs.Any() {
		t.Fatal("expected creator fields rejected on merchant org")
	}
}
