package payments

import (
	"errors"
	"net/http"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"
)

type PaymentApp struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	FeeType     string    `json:"fee_type"`
	FeePercent  string    `json:"fee_percent"`
	FeeFixed    string    `json:"fee_fixed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PaymentAppListResult struct {
	Items  []PaymentApp `json:"items"`
	Total  int64        `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type PaymentOrder struct {
	ID                    string          `json:"id"`
	AppID                 string          `json:"app_id,omitempty"`
	Provider              string          `json:"provider"`
	ProviderOrderID       string          `json:"provider_order_id,omitempty"`
	ProviderTransactionID string          `json:"provider_transaction_id,omitempty"`
	ExternalReference     string          `json:"external_reference,omitempty"`
	Amount                string          `json:"amount"`
	Currency              string          `json:"currency"`
	BuyerName             string          `json:"buyer_name,omitempty"`
	BuyerEmail            string          `json:"buyer_email,omitempty"`
	BuyerPhone            string          `json:"buyer_phone,omitempty"`
	Status                provider.Status `json:"status"`
	ProviderStatus        string          `json:"provider_status,omitempty"`
	Metadata              map[string]any  `json:"metadata,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type PaymentOrderSearchFilter struct {
	Query             string
	AppID             string
	Provider          string
	Status            provider.Status
	Phone             string
	PaymentID         string
	ProviderOrderID   string
	ExternalReference string
	Limit             int
	Offset            int
}

type PaymentOrderSearchResult struct {
	Items  []PaymentOrder `json:"items"`
	Total  int64          `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type CreatePaymentOrderInput struct {
	Provider          string         `json:"provider"`
	Amount            string         `json:"amount"`
	Currency          string         `json:"currency"`
	BuyerName         string         `json:"buyer_name"`
	BuyerEmail        string         `json:"buyer_email"`
	BuyerPhone        string         `json:"buyer_phone"`
	ExternalReference string         `json:"external_reference"`
	Metadata          map[string]any `json:"metadata"`
}

type CreatePaymentOrderRepositoryInput struct {
	AppID                 string
	Provider              string
	ProviderOrderID       string
	ProviderTransactionID string
	ExternalReference     string
	Amount                string
	Currency              string
	BuyerName             string
	BuyerEmail            string
	BuyerPhone            string
	Status                provider.Status
	ProviderStatus        string
	Metadata              map[string]any
}

type PaymentEventInput struct {
	Provider              string
	EventType             string
	ProviderEventID       string
	ProviderOrderID       string
	ProviderTransactionID string
	SignatureValid        bool
	PayloadHash           string
	DedupeKey             string
	Headers               http.Header
	RawBody               string
	NormalizedStatus      provider.Status
	Error                 string
}

type StoredPaymentEvent struct {
	ID        string
	Duplicate bool
}

type PaymentEvent struct {
	ID                    string          `json:"id"`
	Provider              string          `json:"provider"`
	EventType             string          `json:"event_type"`
	ProviderEventID       string          `json:"provider_event_id,omitempty"`
	ProviderOrderID       string          `json:"provider_order_id,omitempty"`
	ProviderTransactionID string          `json:"provider_transaction_id,omitempty"`
	PaymentOrderID        string          `json:"payment_order_id,omitempty"`
	AppID                 string          `json:"app_id,omitempty"`
	ExternalReference     string          `json:"external_reference,omitempty"`
	SignatureValid        bool            `json:"signature_valid"`
	PayloadHash           string          `json:"payload_hash"`
	NormalizedStatus      provider.Status `json:"normalized_status,omitempty"`
	ReceivedAt            time.Time       `json:"received_at"`
	ProcessedAt           *time.Time      `json:"processed_at,omitempty"`
	LastSeenAt            time.Time       `json:"last_seen_at"`
	DuplicateCount        int             `json:"duplicate_count"`
	Error                 string          `json:"error,omitempty"`
}

type PaymentEventListFilter struct {
	Provider        string
	EventType       string
	PaymentOrderID  string
	ProviderOrderID string
	Status          provider.Status
	Processed       *bool
	HasError        *bool
	SignatureValid  *bool
	Limit           int
	Offset          int
}

type PaymentEventListResult struct {
	Items  []PaymentEvent `json:"items"`
	Total  int64          `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type ApplyWebhookEventInput struct {
	EventID               string
	PaymentOrderID        string
	FromStatus            provider.Status
	ToStatus              provider.Status
	ProviderStatus        string
	ProviderTransactionID string
	// Source records how this status change was detected — "webhook",
	// "refresh" (an app explicitly checking one order), or "reconciliation"
	// (the background sweep of stale orders). All three paths call this
	// same method so ledger crediting behaves identically regardless of
	// how the "paid" transition was detected.
	Source string

	// Ledger fields — only used (and only inserted) when PostLedger is true,
	// i.e. this transition is genuinely into "paid" for the first time.
	// Computed by the service layer, which has the app's fee config.
	PostLedger        bool
	AppID             string
	GrossAmount       string
	Currency          string
	PlatformFeeAmount string

	// PostRefund fires when a paid order transitions to reversed — reverses
	// the original payment_credit/platform_fee_debit for this order. See
	// postRefundEntries in repository.go.
	PostRefund bool
}

type WebhookResult struct {
	EventID    string `json:"event_id,omitempty"`
	Duplicate  bool   `json:"duplicate"`
	Processed  bool   `json:"processed"`
	OrderFound bool   `json:"order_found"`
}

type EventProcessingError struct {
	EventID string
	Message string
}

type PaymentStatusHistory struct {
	ID             string
	PaymentOrderID string
	FromStatus     provider.Status
	ToStatus       provider.Status
	ProviderStatus string
	Source         string
	EventID        string
	CreatedAt      time.Time
}

type CreatePaymentAppInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreatePaymentAppResult struct {
	App    PaymentApp `json:"app"`
	APIKey string     `json:"api_key"`
}

type PaymentWebhookEndpoint struct {
	ID         string    `json:"id"`
	AppID      string    `json:"app_id"`
	URL        string    `json:"url"`
	EventTypes []string  `json:"event_types"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreatePaymentWebhookEndpointInput struct {
	AppID      string   `json:"app_id"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

type CreatePaymentWebhookEndpointRepositoryInput struct {
	ID         string
	AppID      string
	URL        string
	EventTypes []string
	SecretHash string
}

type CreatePaymentWebhookEndpointResult struct {
	Endpoint      PaymentWebhookEndpoint `json:"endpoint"`
	SigningSecret string                 `json:"signing_secret"`
}

type ReplayPaymentEventResult struct {
	CreatedDeliveries  int64 `json:"created_deliveries"`
	ReplayedDeliveries int64 `json:"replayed_deliveries"`
	QueuedDeliveries   int64 `json:"queued_deliveries"`
}

type PaymentWebhookDeliveryJob struct {
	ID                    string
	EventID               string
	EndpointID            string
	EndpointURL           string
	AppID                 string
	AttemptCount          int
	EventType             string
	Provider              string
	ProviderOrderID       string
	ProviderTransactionID string
	PaymentOrder          PaymentOrder
	ReceivedAt            time.Time
	RawProviderPayload    string
}

type PaymentWebhookDelivery struct {
	ID                    string       `json:"id"`
	EventID               string       `json:"event_id"`
	EndpointID            string       `json:"endpoint_id"`
	EndpointURL           string       `json:"endpoint_url"`
	AppID                 string       `json:"app_id"`
	Status                string       `json:"status"`
	AttemptCount          int          `json:"attempt_count"`
	NextAttemptAt         time.Time    `json:"next_attempt_at"`
	LastAttemptAt         *time.Time   `json:"last_attempt_at,omitempty"`
	LastResponseStatus    *int         `json:"last_response_status,omitempty"`
	LastError             string       `json:"last_error,omitempty"`
	DeliveredAt           *time.Time   `json:"delivered_at,omitempty"`
	CreatedAt             time.Time    `json:"created_at"`
	EventType             string       `json:"event_type"`
	Provider              string       `json:"provider"`
	ProviderOrderID       string       `json:"provider_order_id,omitempty"`
	ProviderTransactionID string       `json:"provider_transaction_id,omitempty"`
	PaymentOrder          PaymentOrder `json:"payment_order"`
}

type PaymentWebhookDeliveryListFilter struct {
	Status         string
	EventID        string
	EndpointID     string
	PaymentOrderID string
	Limit          int
	Offset         int
}

type PaymentWebhookDeliveryListResult struct {
	Items  []PaymentWebhookDelivery `json:"items"`
	Total  int64                    `json:"total"`
	Limit  int                      `json:"limit"`
	Offset int                      `json:"offset"`
}

type WebhookDeliveryPayload struct {
	ID                string            `json:"id"`
	Type              string            `json:"type"`
	Provider          string            `json:"provider"`
	PaymentID         string            `json:"payment_id"`
	ProviderOrderID   string            `json:"provider_order_id,omitempty"`
	ExternalReference string            `json:"external_reference,omitempty"`
	Status            provider.Status   `json:"status"`
	Amount            string            `json:"amount"`
	Currency          string            `json:"currency"`
	Customer          map[string]string `json:"customer"`
	OccurredAt        time.Time         `json:"occurred_at"`
	Data              map[string]any    `json:"data"`
}

type RecordWebhookDeliveryAttemptInput struct {
	DeliveryID     string
	Success        bool
	Failed         bool
	ResponseStatus int
	Error          string
	NextAttemptAt  *time.Time
}

type ProcessDeliveriesResult struct {
	Claimed   int `json:"claimed"`
	Delivered int `json:"delivered"`
	Retrying  int `json:"retrying"`
	Failed    int `json:"failed"`
}

type ReconcilePaymentsResult struct {
	Scanned           int      `json:"scanned"`
	Checked           int      `json:"checked"`
	Updated           int      `json:"updated"`
	Unchanged         int      `json:"unchanged"`
	Failed            int      `json:"failed"`
	DeliveriesCreated int64    `json:"deliveries_created"`
	Errors            []string `json:"errors,omitempty"`
}

type ReconcilePayoutsResult struct {
	Scanned   int      `json:"scanned"`
	Checked   int      `json:"checked"`
	Updated   int      `json:"updated"`
	Unchanged int      `json:"unchanged"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

type PaymentProviderAccount struct {
	ID                   string         `json:"id"`
	Provider             string         `json:"provider"`
	Name                 string         `json:"name"`
	Environment          string         `json:"environment"`
	BaseURL              string         `json:"base_url,omitempty"`
	CredentialsEncrypted string         `json:"-"`
	IsDefault            bool           `json:"is_default"`
	Status               string         `json:"status"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type CreateProviderAccountInput struct {
	Provider    string            `json:"provider"`
	Name        string            `json:"name"`
	Environment string            `json:"environment"`
	BaseURL     string            `json:"base_url"`
	Credentials map[string]string `json:"credentials"`
}

// UpdateProviderAccountInput is a partial patch — Credentials is only
// re-encrypted when present, so renaming a provider account never requires
// re-entering its API key.
type UpdateProviderAccountInput struct {
	Name        string            `json:"name"`
	Environment string            `json:"environment"`
	BaseURL     string            `json:"base_url"`
	Status      string            `json:"status"`
	Credentials map[string]string `json:"credentials"`
}

// ---------- Ledger, balances, fees, withdrawals ----------
// Phase 1 of the AZSUBAY Gateway ledger: balance is always derived from
// summing ledger entries, never stored as a mutable column. See
// PAYMENTS_GATEWAY_ARCHITECTURE.md for the full design rationale.

const (
	LedgerEntryPaymentCredit            = "payment_credit"
	LedgerEntryPlatformFeeDebit         = "platform_fee_debit"
	LedgerEntryWithdrawalDebit          = "withdrawal_debit"
	LedgerEntryWithdrawalReversalCredit = "withdrawal_reversal_credit"
	LedgerEntryAdjustmentCredit         = "adjustment_credit"
	LedgerEntryAdjustmentDebit          = "adjustment_debit"

	LedgerDirectionCredit = "credit"
	LedgerDirectionDebit  = "debit"

	WithdrawalStatusRequested  = "requested"
	WithdrawalStatusApproved   = "approved"
	WithdrawalStatusProcessing = "processing"
	WithdrawalStatusRejected   = "rejected"
	WithdrawalStatusPaid       = "paid"
	WithdrawalStatusFailed     = "failed"
)

type UpdateAppFeesInput struct {
	FeeType    string `json:"fee_type"`
	FeePercent string `json:"fee_percent"`
	FeeFixed   string `json:"fee_fixed"`
}

type AppBalance struct {
	AppID             string `json:"app_id"`
	Currency          string `json:"currency"`
	AvailableBalance  string `json:"available_balance"`
	TotalRevenue      string `json:"total_revenue"`
	TotalPlatformFees string `json:"total_platform_fees"`
	TotalWithdrawn    string `json:"total_withdrawn"`
	PendingOrderTotal string `json:"pending_order_total"`
	// AvailableBalanceSevenDaysAgo is the same ledger-derived balance
	// formula (SUM(credit) - SUM(debit)) but only over entries at least 7
	// days old — a real week-over-week comparison point, not a stored or
	// estimated figure, so the mobile dashboard's trend line is never
	// fabricated demo data.
	AvailableBalanceSevenDaysAgo string `json:"available_balance_seven_days_ago"`
}

type LedgerEntry struct {
	ID             string    `json:"id"`
	AppID          string    `json:"app_id"`
	PaymentOrderID string    `json:"payment_order_id,omitempty"`
	WithdrawalID   string    `json:"withdrawal_id,omitempty"`
	EntryType      string    `json:"entry_type"`
	Direction      string    `json:"direction"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	Description    string    `json:"description"`
	CreatedBy      string    `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateLedgerEntryInput struct {
	AppID          string
	PaymentOrderID string
	WithdrawalID   string
	EntryType      string
	Direction      string
	Amount         string
	Currency       string
	Description    string
	CreatedBy      string
}

type LedgerEntryListFilter struct {
	AppID  string
	Limit  int
	Offset int
}

type LedgerEntryListResult struct {
	Items  []LedgerEntry `json:"items"`
	Total  int64         `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type PaymentWithdrawal struct {
	ID                 string         `json:"id"`
	AppID              string         `json:"app_id"`
	Amount             string         `json:"amount"`
	Currency           string         `json:"currency"`
	DestinationType    string         `json:"destination_type"`
	DestinationDetails map[string]any `json:"destination_details"`
	Status             string         `json:"status"`
	RequestedBy        string         `json:"requested_by"`
	ApprovedBy         string         `json:"approved_by,omitempty"`
	Notes              string         `json:"notes,omitempty"`
	Provider           string         `json:"provider,omitempty"`
	ProviderPayoutID   string         `json:"provider_payout_id,omitempty"`
	ProviderStatus     string         `json:"provider_status,omitempty"`
	FailureReason      string         `json:"failure_reason,omitempty"`
	DispatchedAt       *time.Time     `json:"dispatched_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type CreateWithdrawalInput struct {
	AppID              string
	Amount             string
	Currency           string
	DestinationType    string
	DestinationDetails map[string]any
	Notes              string
	RequestedBy        string
}

type WithdrawalListFilter struct {
	AppID  string
	Status string
	Limit  int
	Offset int
}

type WithdrawalListResult struct {
	Items  []PaymentWithdrawal `json:"items"`
	Total  int64               `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

var ErrInsufficientBalance = errors.New("insufficient available balance")
var ErrWithdrawalNotFound = errors.New("withdrawal not found")
var ErrInvalidWithdrawalTransition = errors.New("invalid withdrawal status transition")
var ErrOrderNotRefundable = errors.New("order is not in a refundable state")
var ErrAlreadyRefunded = errors.New("order has already been refunded")
var ErrWithdrawalNotApproved = errors.New("withdrawal must be approved before it can be dispatched for payout")
var ErrWithdrawalAlreadyDispatched = errors.New("withdrawal has already been dispatched to the payout provider")
var ErrProviderDoesNotSupportPayouts = errors.New("payment provider does not support automated payouts")

// ---------- Merchant identity & access (Phase 2) ----------
// Links a service-managed user to a specific payment_app.
// No merchant-facing routes exist yet — this is the data model and
// admin-side management only. See PAYMENTS_GATEWAY_ARCHITECTURE.md.

var ErrAlreadyMember = errors.New("user is already a member of this app")
var ErrUserNotFound = errors.New("no account found for that email")
var ErrAppMemberNotFound = errors.New("app member not found")

type AppMember struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	AddedBy   string    `json:"added_by"`
	CreatedAt time.Time `json:"created_at"`
}

type PaymentMetrics struct {
	GeneratedAt              time.Time        `json:"generated_at"`
	OrdersByStatus           map[string]int64 `json:"orders_by_status"`
	DeliveriesByStatus       map[string]int64 `json:"deliveries_by_status"`
	TotalEvents              int64            `json:"total_events"`
	InvalidSignatureEvents   int64            `json:"invalid_signature_events"`
	DuplicateCallbacks       int64            `json:"duplicate_callbacks"`
	UnprocessedEvents        int64            `json:"unprocessed_events"`
	EventErrors              int64            `json:"event_errors"`
	ReconciliationCandidates int64            `json:"reconciliation_candidates"`
}
