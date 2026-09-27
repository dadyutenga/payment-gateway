package sandbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"lipago/internal/modules/payments/provider"
)

func TestConfigFromCredentials(t *testing.T) {
	cfg := ConfigFromCredentials(nil)
	if cfg.SettleSeconds != 30 || cfg.AlwaysFail {
		t.Fatalf("defaults: %+v", cfg)
	}
	cfg = ConfigFromCredentials(map[string]string{"settle_seconds": "5"})
	if cfg.SettleSeconds != 5 {
		t.Fatalf("settle: %+v", cfg)
	}
	cfg = ConfigFromCredentials(map[string]string{"settle_seconds": "bogus", "always_fail": "TRUE"})
	if cfg.SettleSeconds != 30 || !cfg.AlwaysFail {
		t.Fatalf("fallbacks: %+v", cfg)
	}
}

func TestCreateOrderMintsPending(t *testing.T) {
	p := New(Config{SettleSeconds: 30})
	order, err := p.CreateOrder(context.Background(), provider.CreateOrderRequest{
		Amount: "1000.00", Currency: "tzs", BuyerPhone: "+255712345678", ExternalReference: "ext-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if order.Status != provider.StatusPending {
		t.Fatalf("status = %q, want pending", order.Status)
	}
	if order.Currency != "TZS" || order.Reference != "ext-1" || order.Phone != "+255712345678" {
		t.Fatalf("echo fields: %+v", order)
	}
	if _, _, err := parseOrderID(order.OrderID); err != nil {
		t.Fatalf("order id not parseable: %q (%v)", order.OrderID, err)
	}
	if order.TransactionID == "" {
		t.Fatal("expected transaction id")
	}
}

func TestCheckOrderStatusSettlesByAge(t *testing.T) {
	p := New(Config{SettleSeconds: 60})

	fresh, _ := p.CreateOrder(context.Background(), provider.CreateOrderRequest{Amount: "1"})
	status, err := p.CheckOrderStatus(context.Background(), fresh.OrderID)
	if err != nil {
		t.Fatalf("check fresh: %v", err)
	}
	if status.Status != provider.StatusPending {
		t.Fatalf("fresh order status = %q, want pending", status.Status)
	}

	oldID := fmt.Sprintf("sbx_%d_%s", time.Now().UTC().Add(-2*time.Minute).Unix(), "0123456789abcdef")
	status, err = p.CheckOrderStatus(context.Background(), oldID)
	if err != nil {
		t.Fatalf("check old: %v", err)
	}
	if status.Status != provider.StatusPaid {
		t.Fatalf("old order status = %q, want paid", status.Status)
	}
	if status.TransactionID != "sbx_txn_0123456789abcdef" {
		t.Fatalf("transaction id must be stable across checks, got %q", status.TransactionID)
	}

	instant := New(Config{SettleSeconds: 0})
	status, err = instant.CheckOrderStatus(context.Background(), fresh.OrderID)
	if err != nil {
		t.Fatalf("check instant: %v", err)
	}
	if status.Status != provider.StatusPaid {
		t.Fatalf("settle_seconds=0 must pay on first check, got %q", status.Status)
	}
}

func TestCheckOrderStatusRejectsForeignIDs(t *testing.T) {
	p := New(Config{})
	for _, id := range []string{"", "sp_123", "sbx_notatime_0123456789abcdef", "sbx_123_short", "sbx_-5_0123456789abcdef", "sbx_123_zzzzzzzzzzzzzzzz"} {
		if _, err := p.CheckOrderStatus(context.Background(), id); err == nil {
			t.Fatalf("expected error for %q", id)
		}
	}
}

func TestCheckOrderStatusAlwaysFail(t *testing.T) {
	p := New(Config{SettleSeconds: 0, AlwaysFail: true})
	oldID := fmt.Sprintf("sbx_%d_%s", time.Now().UTC().Add(-time.Hour).Unix(), "0123456789abcdef")
	status, err := p.CheckOrderStatus(context.Background(), oldID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if status.Status != provider.StatusFailed {
		t.Fatalf("status = %q, want failed", status.Status)
	}
}

func TestRefundReversesInstantly(t *testing.T) {
	p := New(Config{})
	result, err := p.RefundOrder(context.Background(), nil, "ext-1", "1000.00", "TZS", "test")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if result.Status != provider.StatusReversed || result.ProviderRefundID == "" {
		t.Fatalf("unexpected refund result: %+v", result)
	}
}

func TestNoInboundWebhooks(t *testing.T) {
	p := New(Config{})
	if err := p.VerifyWebhook(nil, []byte("{}")); err == nil {
		t.Fatal("expected VerifyWebhook error")
	}
	if _, err := p.ParseWebhook([]byte("{}")); err == nil {
		t.Fatal("expected ParseWebhook error")
	}
	if p.Name() != "sandbox" {
		t.Fatalf("name = %q", p.Name())
	}
}
