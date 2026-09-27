package payments

import (
	"context"
	"errors"
	"testing"
)

func TestEffectiveLiveCaps(t *testing.T) {
	newSvc := func(repo *fakePaymentRepository) *Service {
		svc, _ := newServiceWithFake(t, ServiceOptions{
			LiveMaxTxnAmount:   "5000000",
			LiveDailyVolumeCap: "50000000",
		})
		svc.repo = repo
		return svc
	}

	// No org → platform defaults.
	svc := newSvc(&fakePaymentRepository{})
	maxTxn, dailyCap, err := svc.effectiveLiveCaps(context.Background(), PaymentApp{ID: "app_test"})
	if err != nil {
		t.Fatalf("caps: %v", err)
	}
	if maxTxn != "5000000" || dailyCap != "50000000" {
		t.Fatalf("defaults: %q / %q", maxTxn, dailyCap)
	}

	// Org overrides win.
	svc = newSvc(&fakePaymentRepository{orgLiveMaxTxn: "1000", orgLiveDailyCap: "2000"})
	maxTxn, dailyCap, err = svc.effectiveLiveCaps(context.Background(), PaymentApp{ID: "app_test", OrgID: "org_test"})
	if err != nil {
		t.Fatalf("caps: %v", err)
	}
	if maxTxn != "1000" || dailyCap != "2000" {
		t.Fatalf("overrides: %q / %q", maxTxn, dailyCap)
	}

	// Empty override sides fall back individually.
	svc = newSvc(&fakePaymentRepository{orgLiveMaxTxn: "", orgLiveDailyCap: "2000"})
	maxTxn, dailyCap, err = svc.effectiveLiveCaps(context.Background(), PaymentApp{ID: "app_test", OrgID: "org_test"})
	if err != nil || maxTxn != "5000000" || dailyCap != "2000" {
		t.Fatalf("partial: %q / %q, err %v", maxTxn, dailyCap, err)
	}

	// Garbage stored values fail open to the default (logged), never to
	// zero — a typo must not forbid all live volume.
	svc = newSvc(&fakePaymentRepository{orgLiveMaxTxn: "bogus", orgLiveDailyCap: "-5"})
	maxTxn, dailyCap, err = svc.effectiveLiveCaps(context.Background(), PaymentApp{ID: "app_test", OrgID: "org_test"})
	if err != nil || maxTxn != "5000000" || dailyCap != "50000000" {
		t.Fatalf("invalid fallback: %q / %q, err %v", maxTxn, dailyCap, err)
	}

	// Lookup errors propagate (data inconsistency, not a default).
	svc = newSvc(&fakePaymentRepository{orgLimitsErr: errors.New("db down")})
	if _, _, err := svc.effectiveLiveCaps(context.Background(), PaymentApp{ID: "app_test", OrgID: "org_test"}); err == nil {
		t.Fatal("expected lookup error")
	}
}

func TestCreateOrderHonorsOrgTxnCap(t *testing.T) {
	repo := &fakePaymentRepository{orgLiveMaxTxn: "500", orgLiveDailyCap: "50000000"}
	svc, _ := newServiceWithFake(t, ServiceOptions{LiveMaxTxnAmount: "5000000", LiveDailyVolumeCap: "50000000"})
	svc.repo = repo
	app := PaymentApp{ID: "app_test", OrgID: "org_test", Status: "active"}

	// 1000 is under the platform default but over the org override.
	_, _, err := svc.CreateOrder(context.Background(), app, CreatePaymentOrderInput{
		Provider: "sonicpesa", Amount: "1000", Currency: "TZS",
		BuyerName: "A", BuyerEmail: "a@example.com", BuyerPhone: "+255712345678",
		Environment: "live",
	})
	if !errors.Is(err, ErrLiveTxnCapExceeded) {
		t.Fatalf("expected org cap to trip, got %v", err)
	}

	// Under the org cap passes the cap stage (fake provider path after).
	_, _, err = svc.CreateOrder(context.Background(), app, CreatePaymentOrderInput{
		Provider: "sonicpesa", Amount: "100", Currency: "TZS",
		BuyerName: "A", BuyerEmail: "a@example.com", BuyerPhone: "+255712345678",
		Environment: "live",
	})
	if errors.Is(err, ErrLiveTxnCapExceeded) || errors.Is(err, ErrLiveDailyCapExceeded) {
		t.Fatalf("unexpected cap error: %v", err)
	}
}
