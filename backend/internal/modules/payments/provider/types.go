package provider

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusPaid       Status = "paid"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
	StatusExpired    Status = "expired"
	StatusReversed   Status = "reversed"
	StatusUnknown    Status = "unknown"
)

type CreateOrderRequest struct {
	Amount            string
	Currency          string
	BuyerName         string
	BuyerEmail        string
	BuyerPhone        string
	ExternalReference string
	Metadata          map[string]any
}

type ProviderOrder struct {
	OrderID        string
	TransactionID  string
	Reference      string
	Amount         string
	Currency       string
	Phone          string
	Status         Status
	ProviderStatus string
	Raw            map[string]any
}

type ProviderStatus struct {
	OrderID        string
	TransactionID  string
	Reference      string
	Amount         string
	Currency       string
	Phone          string
	Status         Status
	ProviderStatus string
	Raw            map[string]any
}

type WebhookEvent struct {
	EventType             string
	ProviderEventID       string
	ProviderOrderID       string
	ProviderTransactionID string
	ProviderStatus        string
	NormalizedStatus      Status
	Amount                string
	Currency              string
	Phone                 string
	Reference             string
	OccurredAt            *time.Time
	Data                  map[string]any
}

type WebhookProvider interface {
	Name() string
	VerifyWebhook(headers http.Header, rawBody []byte) error
	ParseWebhook(rawBody []byte) (WebhookEvent, error)
}

type PaymentProvider interface {
	WebhookProvider
	CreateOrder(ctx context.Context, req CreateOrderRequest) (ProviderOrder, error)
	CheckOrderStatus(ctx context.Context, providerOrderID string) (ProviderStatus, error)
}

type DisburseRequest struct {
	Amount             string
	Currency           string
	DestinationType    string // "bank" | "mobile_money"
	DestinationDetails map[string]any
	ExternalReference  string // withdrawal ID — used as the provider-side idempotency key
}

type DisburseResult struct {
	ProviderPayoutID string
	Status           Status // Processing (async, awaits webhook/reconciliation), Paid, or Failed
	ProviderStatus   string
	Raw              map[string]any
}

// Disburser is implemented by provider adapters that support automated
// payouts, separate from PaymentProvider so collection-only providers never
// need stub methods. A payout is either confirmed synchronously (Status is
// Paid/Failed on return) or accepted for async processing (Status is
// Processing, resolved later via a payout webhook or CheckPayoutStatus).
type Disburser interface {
	Disburse(ctx context.Context, req DisburseRequest) (DisburseResult, error)
	CheckPayoutStatus(ctx context.Context, providerPayoutID string) (DisburseResult, error)
}

// ErrRefundNotSupported is returned by Refunder implementations whose
// provider documents no refund API. Callers fall back to a local ledger
// reversal (manual attestation) instead of failing the refund.
var ErrRefundNotSupported = errors.New("provider does not support refunds")

// ProviderRefundResult is the outcome of a refund request. Status is
// StatusReversed when the money is confirmed back with the payer,
// StatusProcessing when the provider accepted it asynchronously (a webhook
// or status check confirms it later), or StatusFailed when rejected.
type ProviderRefundResult struct {
	ProviderRefundID string
	Status           Status
	ProviderStatus   string
	Raw              map[string]any
}

// Refunder is implemented by provider adapters with a verified refund API,
// separate from PaymentProvider so adapters without one never need stub
// methods. Credentials are passed per call (unlike collection, which binds
// them at construction) so the caller controls exactly which account's
// credentials fund the reversal.
type Refunder interface {
	RefundOrder(ctx context.Context, credentials map[string]string, externalReference, amount, currency, reason string) (ProviderRefundResult, error)
}

// Constructor builds a PaymentProvider instance from an admin-configured
// app.payment_provider_accounts row's base URL and decrypted credentials.
// Unlike SMS's stateless Provider (credentials passed per-call), payment
// provider adapters bind credentials at construction time, so the registry
// of implemented kinds holds constructors rather than ready-made instances.
type Constructor func(baseURL string, credentials map[string]string) PaymentProvider
