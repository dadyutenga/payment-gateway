// Package sandbox is a deterministic payment-provider simulator for
// merchant integration testing. It implements the full collection +
// refund surface with no network calls and no real money:
//
//   - CreateOrder mints a pending order instantly. The provider order id
//     embeds its creation time (sbx_<unix>_<rand>), which is the entire
//     settlement clock — the adapter is stateless.
//   - CheckOrderStatus reports paid once the order is older than
//     settle_seconds (credential-tunable, default 30), pending before
//     that, failed when always_fail is set.
//   - RefundOrder reverses instantly (simulated).
//
// There are no inbound webhooks: settlement is observed through refresh /
// reconciliation, exactly like a slow real provider. Live orders can never
// use this provider — payments.Service rejects provider=sandbox for live
// environments, so simulated money can never settle real ledger credits.
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"
)

// Config tunes the simulator. Both fields come from the admin-managed
// provider account credentials (settle_seconds, always_fail).
type Config struct {
	// SettleSeconds delays settlement: orders read paid once older than
	// this. Zero or negative settles on the first status check.
	SettleSeconds int
	// AlwaysFail forces failed instead of paid (failure-path testing).
	AlwaysFail bool
}

// ConfigFromCredentials parses admin-stored credentials with safe
// fallbacks: unset/unparsable settle_seconds means 30s.
func ConfigFromCredentials(credentials map[string]string) Config {
	cfg := Config{SettleSeconds: 30}
	if credentials == nil {
		return cfg
	}
	if raw := strings.TrimSpace(credentials["settle_seconds"]); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			cfg.SettleSeconds = parsed
		}
	}
	cfg.AlwaysFail = strings.EqualFold(strings.TrimSpace(credentials["always_fail"]), "true")
	return cfg
}

// Provider is the sandbox simulator. Stateless by construction — all
// settlement state lives in the order id.
type Provider struct {
	cfg Config
}

// New builds the simulator. The base URL is accepted for constructor
// uniformity and ignored (there is nothing to call).
func New(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Name() string { return "sandbox" }

// VerifyWebhook always fails: the simulator emits no inbound webhooks.
func (p *Provider) VerifyWebhook(_ http.Header, _ []byte) error {
	return errors.New("sandbox provider has no inbound webhooks")
}

// ParseWebhook always fails: see VerifyWebhook.
func (p *Provider) ParseWebhook(_ []byte) (provider.WebhookEvent, error) {
	return provider.WebhookEvent{}, errors.New("sandbox provider has no inbound webhooks")
}

// CreateOrder mints a pending simulated order instantly. No network, no
// money — the returned id embeds the creation time for the settle clock.
func (p *Provider) CreateOrder(_ context.Context, req provider.CreateOrderRequest) (provider.ProviderOrder, error) {
	random, err := randomHex(8)
	if err != nil {
		return provider.ProviderOrder{}, fmt.Errorf("mint sandbox order id: %w", err)
	}
	orderID := fmt.Sprintf("sbx_%d_%s", time.Now().UTC().Unix(), random)
	return provider.ProviderOrder{
		OrderID:        orderID,
		TransactionID:  "sbx_txn_" + random,
		Reference:      strings.TrimSpace(req.ExternalReference),
		Amount:         strings.TrimSpace(req.Amount),
		Currency:       strings.ToUpper(strings.TrimSpace(req.Currency)),
		Phone:          strings.TrimSpace(req.BuyerPhone),
		Status:         provider.StatusPending,
		ProviderStatus: "sandbox_pending",
		Raw:            map[string]any{"sandbox": true, "settle_seconds": p.cfg.SettleSeconds},
	}, nil
}

// CheckOrderStatus settles by age: failed when always_fail is set, paid
// once older than settle_seconds, pending otherwise. Malformed ids error
// instead of inventing money.
func (p *Provider) CheckOrderStatus(_ context.Context, providerOrderID string) (provider.ProviderStatus, error) {
	created, random, err := parseOrderID(providerOrderID)
	if err != nil {
		return provider.ProviderStatus{}, err
	}
	status := provider.StatusPending
	providerStatus := "sandbox_pending"
	switch {
	case p.cfg.AlwaysFail:
		status = provider.StatusFailed
		providerStatus = "sandbox_failed"
	case time.Now().UTC().Unix()-created >= int64(p.cfg.SettleSeconds):
		status = provider.StatusPaid
		providerStatus = "sandbox_paid"
	}
	return provider.ProviderStatus{
		OrderID:        strings.TrimSpace(providerOrderID),
		TransactionID:  "sbx_txn_" + random,
		Status:         status,
		ProviderStatus: providerStatus,
		Raw:            map[string]any{"sandbox": true},
	}, nil
}

// RefundOrder reverses instantly (simulated). Mirrors the Refunder
// contract: reversed means money confirmed back with the payer.
func (p *Provider) RefundOrder(_ context.Context, _ map[string]string, externalReference, amount, currency, _ string) (provider.ProviderRefundResult, error) {
	random, err := randomHex(8)
	if err != nil {
		return provider.ProviderRefundResult{}, fmt.Errorf("mint sandbox refund id: %w", err)
	}
	return provider.ProviderRefundResult{
		ProviderRefundID: "sbx_rfnd_" + random,
		Status:           provider.StatusReversed,
		ProviderStatus:   "sandbox_reversed",
	}, nil
}

// parseOrderID splits sbx_<unix>_<16 hex>. Anything else is rejected —
// status checks must never settle orders this simulator didn't mint.
func parseOrderID(providerOrderID string) (created int64, random string, err error) {
	parts := strings.Split(strings.TrimSpace(providerOrderID), "_")
	if len(parts) != 3 || parts[0] != "sbx" {
		return 0, "", fmt.Errorf("not a sandbox order id: %q", providerOrderID)
	}
	created, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil || created <= 0 {
		return 0, "", fmt.Errorf("not a sandbox order id: %q", providerOrderID)
	}
	if len(parts[2]) != 16 {
		return 0, "", fmt.Errorf("not a sandbox order id: %q", providerOrderID)
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return 0, "", fmt.Errorf("not a sandbox order id: %q", providerOrderID)
	}
	return created, parts[2], nil
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
