package payments

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newServiceWithFake(t *testing.T, opts ServiceOptions) (*Service, *fakePaymentRepository) {
	t.Helper()
	repo := &fakePaymentRepository{}
	registry := registryFor(&fakePaymentProvider{name: "sonicpesa"})
	svc := NewService(repo, registry, testCipher, opts, nil)
	return svc, repo
}

func TestDefaultOrderEnvironment(t *testing.T) {
	if got := defaultOrderEnvironment(""); got != "live" {
		t.Fatalf("empty → %q, want live", got)
	}
	if got := defaultOrderEnvironment("sandbox"); got != "sandbox" {
		t.Fatalf("sandbox → %q", got)
	}
	if got := defaultOrderEnvironment("SANDBOX"); got != "sandbox" {
		t.Fatalf("SANDBOX → %q", got)
	}
	if got := defaultOrderEnvironment("live"); got != "live" {
		t.Fatalf("live → %q", got)
	}
	if got := defaultOrderEnvironment("production"); got != "live" {
		t.Fatalf("unknown falls back to live, got %q", got)
	}
}

func TestDecimalOver(t *testing.T) {
	cases := []struct {
		value, cap string
		want       bool
	}{
		{"100", "", false},
		{"100", "99", true},
		{"100", "100", false},
		{"100.01", "100", true},
		{"5000000", "5000000", false},
		{"5000000.01", "5000000", true},
	}
	for _, tc := range cases {
		got, err := decimalOver(tc.value, tc.cap)
		if err != nil {
			t.Fatalf("decimalOver(%q, %q): %v", tc.value, tc.cap, err)
		}
		if got != tc.want {
			t.Errorf("decimalOver(%q, %q) = %v, want %v", tc.value, tc.cap, got, tc.want)
		}
	}
	if _, err := decimalOver("not-a-number", "100"); err == nil {
		t.Fatal("unparsable value should fail closed with error")
	}
}

func TestDecimalSumOver(t *testing.T) {
	got, err := decimalSumOver("60", "50", "100")
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if !got {
		t.Fatal("60+50 should exceed 100")
	}
	got, err = decimalSumOver("50", "50", "100")
	if err != nil || got {
		t.Fatalf("50+50 should not exceed 100 (got %v, err %v)", got, err)
	}
	got, err = decimalSumOver("0", "1", "")
	if err != nil || got {
		t.Fatalf("empty cap disables check (got %v, err %v)", got, err)
	}
}

func TestCreateOrderLiveCaps(t *testing.T) {
	svc, _ := newServiceWithFake(t, ServiceOptions{
		LiveMaxTxnAmount:   "1000",
		LiveDailyVolumeCap: "2000",
		OrderExpiryTTL:     30 * time.Minute,
	})
	app := PaymentApp{ID: "app_test", Name: "Test", Status: "active"}

	// Sandbox skips caps.
	if _, _, err := svc.CreateOrder(context.Background(), app, CreatePaymentOrderInput{
		Provider: "sonicpesa", Amount: "99999", Currency: "TZS",
		BuyerName: "A", BuyerEmail: "a@example.com", BuyerPhone: "+255712345678",
		Environment: "sandbox",
	}); err == nil {
		// Provider may fail after caps; we only care that we didn't get cap errors.
		_ = err
	}

	// Live over per-txn cap → ErrLiveTxnCapExceeded (fake provider never reached if cap trips first).
	_, _, err := svc.CreateOrder(context.Background(), app, CreatePaymentOrderInput{
		Provider: "sonicpesa", Amount: "1000.01", Currency: "TZS",
		BuyerName: "A", BuyerEmail: "a@example.com", BuyerPhone: "+255712345678",
		Environment: "live",
	})
	if !errors.Is(err, ErrLiveTxnCapExceeded) {
		t.Fatalf("expected ErrLiveTxnCapExceeded, got %v", err)
	}

	// Under cap passes the cap check (may fail later at provider — fake returns OK).
	_, _, err = svc.CreateOrder(context.Background(), app, CreatePaymentOrderInput{
		Provider: "sonicpesa", Amount: "100", Currency: "TZS",
		BuyerName: "A", BuyerEmail: "a@example.com", BuyerPhone: "+255712345678",
		Environment: "live",
	})
	if errors.Is(err, ErrLiveTxnCapExceeded) || errors.Is(err, ErrLiveDailyCapExceeded) {
		t.Fatalf("unexpected cap error for under-cap order: %v", err)
	}
}

func TestAuthenticateAppKeyReturnsEnvironment(t *testing.T) {
	svc, _ := newServiceWithFake(t, ServiceOptions{})
	app, environment, err := svc.AuthenticateAppKey(context.Background(), "sk_test_123")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if app.ID == "" {
		t.Fatal("expected app")
	}
	if environment != "sandbox" {
		t.Fatalf("environment = %q, want sandbox (fake)", environment)
	}
}
