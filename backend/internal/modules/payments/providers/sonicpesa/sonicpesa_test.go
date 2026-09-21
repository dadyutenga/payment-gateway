package sonicpesa

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"azsubay-payments-gateway/internal/modules/payments/provider"
)

func TestVerifyWebhookSignature(t *testing.T) {
	rawBody := []byte(`{"event":"payment.completed","order_id":"sp_123","status":"SUCCESS"}`)
	secret := "test-secret"
	p := New(Config{APISecret: secret})

	headers := http.Header{}
	headers.Set("X-SonicPesa-Signature", sign(secret, rawBody))

	if err := p.VerifyWebhook(headers, rawBody); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestVerifyWebhookRejectsInvalidSignature(t *testing.T) {
	p := New(Config{APISecret: "test-secret"})
	headers := http.Header{}
	headers.Set("X-SonicPesa-Signature", "bad-signature")

	if err := p.VerifyWebhook(headers, []byte(`{"order_id":"sp_123"}`)); err != ErrSignatureMismatch {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestParseWebhook(t *testing.T) {
	p := New(Config{APISecret: "test-secret"})
	event, err := p.ParseWebhook([]byte(`{
		"event":"payment.completed",
		"order_id":"sp_123",
		"amount":10000,
		"currency":"TZS",
		"status":"SUCCESS",
		"transid":"TXN123",
		"channel":"AIRTELMONEY",
		"reference":"REF123",
		"msisdn":"255700000000",
		"timestamp":"2026-06-04T10:00:00Z"
	}`))
	if err != nil {
		t.Fatalf("expected parse success, got %v", err)
	}
	if event.ProviderOrderID != "sp_123" {
		t.Fatalf("expected order id sp_123, got %q", event.ProviderOrderID)
	}
	if event.NormalizedStatus != provider.StatusPaid {
		t.Fatalf("expected paid status, got %q", event.NormalizedStatus)
	}
	if event.ProviderTransactionID != "TXN123" {
		t.Fatalf("expected transaction id TXN123, got %q", event.ProviderTransactionID)
	}
	if event.Amount != "10000" {
		t.Fatalf("expected amount 10000, got %q", event.Amount)
	}
	if event.OccurredAt == nil {
		t.Fatal("expected occurred_at timestamp to parse")
	}
}

func TestNormalizeStatus(t *testing.T) {
	cases := map[string]provider.Status{
		"PENDING":       provider.StatusPending,
		"INPROGRESS":    provider.StatusProcessing,
		"SUCCESS":       provider.StatusPaid,
		"CANCELLED":     provider.StatusCancelled,
		"USERCANCELLED": provider.StatusCancelled,
		"REJECTED":      provider.StatusFailed,
		"unexpected":    provider.StatusUnknown,
	}

	for input, expected := range cases {
		if got := NormalizeStatus(input); got != expected {
			t.Fatalf("NormalizeStatus(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestCreateOrderPostsToSonicPesa(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment/create_order" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("X-API-KEY"); got != "test-key" {
			t.Fatalf("expected X-API-KEY test-key, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["buyer_phone"] != "255700000000" {
			t.Fatalf("expected buyer phone in request, got %v", payload["buyer_phone"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"message":"Payment order created successfully! Push USSD sent to your phone.",
			"data":{
				"order_id":"sp_123",
				"reference":"S123",
				"amount":10000,
				"currency":"TZS",
				"payment_status":"PENDING",
				"transid":null,
				"msisdn":"255700000000"
			}
		}`))
	}))
	defer server.Close()

	p := NewWithHTTPClient(Config{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	order, err := p.CreateOrder(context.Background(), provider.CreateOrderRequest{
		Amount:     "10000.00",
		Currency:   "TZS",
		BuyerName:  "Customer",
		BuyerEmail: "customer@example.com",
		BuyerPhone: "255700000000",
	})
	if err != nil {
		t.Fatalf("expected create order success, got %v", err)
	}
	if order.OrderID != "sp_123" {
		t.Fatalf("expected order id sp_123, got %q", order.OrderID)
	}
	if order.Status != provider.StatusPending {
		t.Fatalf("expected pending, got %q", order.Status)
	}
	if order.Reference != "S123" {
		t.Fatalf("expected reference S123, got %q", order.Reference)
	}
}

func TestCheckOrderStatusPostsToSonicPesa(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment/order_status" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("X-API-KEY"); got != "test-key" {
			t.Fatalf("expected X-API-KEY test-key, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"message":"Order status retrieved successfully",
			"data":{
				"order_id":"sp_123",
				"payment_status":"SUCCESS",
				"amount":10000,
				"currency":"TZS",
				"phone":"255700000000",
				"transid":"TXN123",
				"reference":"REF123"
			}
		}`))
	}))
	defer server.Close()

	p := NewWithHTTPClient(Config{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	status, err := p.CheckOrderStatus(context.Background(), "sp_123")
	if err != nil {
		t.Fatalf("expected status success, got %v", err)
	}
	if status.Status != provider.StatusPaid {
		t.Fatalf("expected paid, got %q", status.Status)
	}
	if status.TransactionID != "TXN123" {
		t.Fatalf("expected transaction id TXN123, got %q", status.TransactionID)
	}
}

func sign(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}
