package orgs

import (
	"context"
	"errors"
	"testing"
)

func TestReviewKYCApprove(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	org, vErrs, err := service.ReviewKYC(ctx, "admin@example.com", "org_test", true, "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if vErrs.Any() {
		t.Fatalf("unexpected validation errors: %v", vErrs)
	}
	if org.KYCStatus != "verified" {
		t.Fatalf("status = %q, want verified", org.KYCStatus)
	}
	if repo.reviewedStatus != "verified" || repo.reviewedBy != "admin@example.com" {
		t.Fatalf("review not recorded: %+v", repo)
	}
}

func TestReviewKYCRejectRequiresReason(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	if _, vErrs, err := service.ReviewKYC(ctx, "admin@example.com", "org_test", false, ""); err != nil || !vErrs.Any() {
		t.Fatalf("expected reason-required errors, got vErrs=%v err=%v", vErrs, err)
	}

	org, vErrs, err := service.ReviewKYC(ctx, "admin@example.com", "org_test", false, "Blurry document.")
	if err != nil || vErrs.Any() {
		t.Fatalf("reject: vErrs=%v err=%v", vErrs, err)
	}
	if org.KYCStatus != "rejected" || repo.reviewReason != "Blurry document." {
		t.Fatalf("rejection not recorded: %+v / %+v", org, repo)
	}

	repo.reviewErr = ErrKYCNotInReview
	if _, _, err := service.ReviewKYC(ctx, "admin@example.com", "org_test", true, ""); !errors.Is(err, ErrKYCNotInReview) {
		t.Fatalf("expected ErrKYCNotInReview, got %v", err)
	}
}

func TestListKYCQueueStatuses(t *testing.T) {
	repo := ownerRepo()
	repo.queue = []KYCQueueItem{{OrgID: "org_test", KYCStatus: "submitted"}}
	service := NewService(repo, nil)
	ctx := context.Background()

	for _, status := range []string{"", "submitted", "verified", "rejected", "pending", "all"} {
		items, err := service.ListKYCQueue(ctx, status)
		if err != nil {
			t.Fatalf("status %q: %v", status, err)
		}
		if len(items) != 1 {
			t.Fatalf("status %q: got %d items", status, len(items))
		}
	}
	if _, err := service.ListKYCQueue(ctx, "bogus"); !errors.Is(err, ErrKYCQueueStatusUnknown) {
		t.Fatalf("expected ErrKYCQueueStatusUnknown, got %v", err)
	}
}

func TestValidPositiveDecimal(t *testing.T) {
	for _, ok := range []string{"", "1", "0.01", "5000000", "  250 "} {
		if !validPositiveDecimal(ok) {
			t.Errorf("expected %q valid", ok)
		}
	}
	for _, bad := range []string{"0", "-5", "abc", "1,000", "12.34.56"} {
		if validPositiveDecimal(bad) {
			t.Errorf("expected %q invalid", bad)
		}
	}
}

func TestUpdateOrgLiveLimits(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	org, vErrs, err := service.UpdateOrgLiveLimits(ctx, "org_test", "1000", "")
	if err != nil || vErrs.Any() {
		t.Fatalf("update: vErrs=%v err=%v", vErrs, err)
	}
	if org.LiveMaxTxnAmount != "1000" || org.LiveDailyVolumeCap != "" {
		t.Fatalf("limits not stored: %+v", org)
	}
	if repo.limitsMaxTxn != "1000" || repo.limitsDailyCap != "" {
		t.Fatalf("repo got wrong values: %+v", repo)
	}

	if _, vErrs, err := service.UpdateOrgLiveLimits(ctx, "org_test", "0", "lots"); err != nil || !vErrs.Any() {
		t.Fatalf("expected validation errors, got vErrs=%v err=%v", vErrs, err)
	} else {
		if _, ok := vErrs["live_max_txn_amount"]; !ok {
			t.Errorf("expected error on live_max_txn_amount: %v", vErrs)
		}
		if _, ok := vErrs["live_daily_volume_cap"]; !ok {
			t.Errorf("expected error on live_daily_volume_cap: %v", vErrs)
		}
	}
}
