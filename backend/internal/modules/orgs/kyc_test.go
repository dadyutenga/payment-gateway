package orgs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidTIN(t *testing.T) {
	valid := []string{"123456789", "123-456-789", "1234567890123456789"}
	invalid := []string{"", "12345678", "abcdefghijklmn", "123456789012345678901"}
	for _, tin := range valid {
		if !validTIN(tin) {
			t.Errorf("expected TIN %q valid", tin)
		}
	}
	for _, tin := range invalid {
		if validTIN(tin) {
			t.Errorf("expected TIN %q invalid", tin)
		}
	}
}

func TestSubmitKYCRequiresPermissionAndFields(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	// Non-member blocked.
	_, vErrs, err := service.SubmitKYC(ctx, "stranger", "org_test", "Acme", "123456789", "uploads/kyc/doc.pdf")
	if err == nil && !vErrs.Any() {
		t.Fatal("expected non-member blocked")
	}

	// Missing fields → validation errors, no repo write.
	_, vErrs, err = service.SubmitKYC(ctx, "owner-1", "org_test", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vErrs.Any() {
		t.Fatal("expected validation errors for empty fields")
	}
	for _, field := range []string{"business_name", "tin", "id_document_url"} {
		if _, ok := vErrs[field]; !ok {
			t.Errorf("expected error on %s", field)
		}
	}

	// Valid submission succeeds.
	sub, vErrs, err := service.SubmitKYC(ctx, "owner-1", "org_test", "Acme Ltd", "123456789", "uploads/kyc/doc.pdf")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if vErrs.Any() {
		t.Fatalf("unexpected validation errors: %v", vErrs)
	}
	if sub.BusinessName != "Acme Ltd" || sub.IDDocumentURL == "" {
		t.Fatalf("unexpected submission: %+v", sub)
	}
}

func TestGetKYCSubmission(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	// Not submitted → ErrKYCNotSubmitted.
	if _, _, err := service.GetKYCSubmission(ctx, "owner-1", "org_test"); !errors.Is(err, ErrKYCNotSubmitted) {
		t.Fatalf("expected ErrKYCNotSubmitted, got %v", err)
	}

	repo.kycFound = true
	sub, status, err := service.GetKYCSubmission(ctx, "owner-1", "org_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sub.OrgID == "" || status == "" {
		t.Fatalf("expected submission + status, got sub=%+v status=%q", sub, status)
	}

	// Viewer can read; stranger cannot.
	repo.members[orgKey("org_test", "u-viewer")] = OrgMember{OrgID: "org_test", UserID: "u-viewer", Role: RoleViewer, Status: MemberStatusActive}
	if _, _, err := service.GetKYCSubmission(ctx, "u-viewer", "org_test"); err != nil {
		t.Fatalf("viewer read: %v", err)
	}
	if _, _, err := service.GetKYCSubmission(ctx, "stranger", "org_test"); err == nil {
		t.Fatal("stranger blocked")
	}
}

func TestOrgKYCStatus(t *testing.T) {
	repo := ownerRepo()
	repo.org = Organization{ID: "org_test", Name: "Test", KYCStatus: "verified"}
	service := NewService(repo, nil)

	status, err := service.OrgKYCStatus(context.Background(), "app_test")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "verified" {
		t.Fatalf("status = %q, want verified", status)
	}
	if !strings.EqualFold(status, "verified") {
		t.Fatal("status comparison should be case-insensitive-friendly")
	}
}
