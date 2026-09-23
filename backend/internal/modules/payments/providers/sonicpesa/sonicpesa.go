package sonicpesa

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"
)

var (
	ErrWebhookSecretMissing = errors.New("sonicpesa webhook secret missing")
	ErrSignatureMissing     = errors.New("sonicpesa signature missing")
	ErrSignatureMismatch    = errors.New("sonicpesa signature mismatch")
	ErrAPIKeyMissing        = errors.New("sonicpesa api key missing")
)

type Config struct {
	BaseURL   string
	APIKey    string
	APISecret string
}

type Provider struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Provider {
	return NewWithHTTPClient(cfg, &http.Client{Timeout: 10 * time.Second})
}

func NewWithHTTPClient(cfg Config, client *http.Client) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{
		cfg:  cfg,
		http: client,
	}
}

func (p *Provider) Name() string {
	return "sonicpesa"
}

func (p *Provider) VerifyWebhook(headers http.Header, rawBody []byte) error {
	secret := strings.TrimSpace(p.cfg.APISecret)
	if secret == "" {
		return ErrWebhookSecretMissing
	}

	signature := normalizeSignature(headers.Get("X-SonicPesa-Signature"))
	if signature == "" {
		return ErrSignatureMissing
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return ErrSignatureMismatch
	}

	return nil
}

func (p *Provider) ParseWebhook(rawBody []byte) (provider.WebhookEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return provider.WebhookEvent{}, fmt.Errorf("decode sonicpesa webhook: %w", err)
	}

	eventType := stringField(payload, "event", "type")

	// Payout/disbursement callbacks are detected by event name or by the
	// presence of a payout-specific identifier field, since the exact
	// SonicPesa payout webhook shape is unverified (see Disburse doc
	// comment below). ProviderOrderID is repurposed to carry the payout ID
	// for this branch so callers can look up the withdrawal the same way
	// they look up an order for a payment webhook.
	payoutID := stringField(payload, "payout_id", "disbursement_id")
	if strings.Contains(strings.ToLower(eventType), "payout") || strings.Contains(strings.ToLower(eventType), "disburs") || payoutID != "" {
		if payoutID == "" {
			return provider.WebhookEvent{}, errors.New("sonicpesa payout webhook missing payout_id")
		}
		providerStatus := stringField(payload, "status", "payout_status")
		return provider.WebhookEvent{
			EventType:             "payout.updated",
			ProviderEventID:       stringField(payload, "event_id", "id"),
			ProviderOrderID:       payoutID,
			ProviderTransactionID: stringField(payload, "transid", "transaction_id"),
			ProviderStatus:        providerStatus,
			NormalizedStatus:      NormalizeStatus(providerStatus),
			Amount:                stringField(payload, "amount"),
			Currency:              stringField(payload, "currency"),
			Phone:                 stringField(payload, "msisdn", "phone"),
			Reference:             stringField(payload, "reference"),
			OccurredAt:            parseTimePtr(stringField(payload, "timestamp", "occurred_at", "updated_at", "created_at")),
			Data:                  payload,
		}, nil
	}

	if eventType == "" {
		eventType = "payment.updated"
	}

	providerOrderID := stringField(payload, "order_id")
	if providerOrderID == "" {
		return provider.WebhookEvent{}, errors.New("sonicpesa webhook missing order_id")
	}

	providerStatus := stringField(payload, "status", "payment_status")

	return provider.WebhookEvent{
		EventType:             eventType,
		ProviderEventID:       stringField(payload, "event_id", "id"),
		ProviderOrderID:       providerOrderID,
		ProviderTransactionID: stringField(payload, "transid", "transaction_id"),
		ProviderStatus:        providerStatus,
		NormalizedStatus:      NormalizeStatus(providerStatus),
		Amount:                stringField(payload, "amount"),
		Currency:              stringField(payload, "currency"),
		Phone:                 stringField(payload, "msisdn", "phone", "buyer_phone"),
		Reference:             stringField(payload, "reference"),
		OccurredAt:            parseTimePtr(stringField(payload, "timestamp", "occurred_at", "updated_at", "created_at")),
		Data:                  payload,
	}, nil
}

func (p *Provider) CreateOrder(ctx context.Context, req provider.CreateOrderRequest) (provider.ProviderOrder, error) {
	payload := map[string]any{
		"buyer_email": req.BuyerEmail,
		"buyer_name":  req.BuyerName,
		"buyer_phone": req.BuyerPhone,
		"amount":      json.Number(req.Amount),
		"currency":    req.Currency,
		// Our idempotency key: lets SonicPesa dedupe retries on their side
		// and lets orphan provider orders (created after our timeout) be
		// matched back to the local pending row during reconciliation.
		"reference": req.ExternalReference,
	}

	parsed, err := p.post(ctx, "/payment/create_order", payload)
	if err != nil {
		return provider.ProviderOrder{}, err
	}

	data := objectField(parsed, "data")
	if len(data) == 0 {
		data = parsed
	}

	status := stringField(data, "payment_status", "status")
	orderID := stringField(data, "order_id")
	if orderID == "" {
		return provider.ProviderOrder{}, errors.New("sonicpesa create order response missing order_id")
	}

	return provider.ProviderOrder{
		OrderID:        orderID,
		TransactionID:  stringField(data, "transid", "transaction_id"),
		Reference:      stringField(data, "reference"),
		Amount:         defaultString(stringField(data, "amount"), req.Amount),
		Currency:       defaultString(stringField(data, "currency"), req.Currency),
		Phone:          defaultString(stringField(data, "msisdn", "phone"), req.BuyerPhone),
		Status:         NormalizeStatus(status),
		ProviderStatus: status,
		Raw:            parsed,
	}, nil
}

func (p *Provider) CheckOrderStatus(ctx context.Context, providerOrderID string) (provider.ProviderStatus, error) {
	providerOrderID = strings.TrimSpace(providerOrderID)
	if providerOrderID == "" {
		return provider.ProviderStatus{}, errors.New("sonicpesa order id is required")
	}

	parsed, err := p.post(ctx, "/payment/order_status", map[string]any{"order_id": providerOrderID})
	if err != nil {
		return provider.ProviderStatus{}, err
	}

	data := objectField(parsed, "data")
	if len(data) == 0 {
		data = parsed
	}
	transaction := objectField(parsed, "transaction")

	status := stringField(data, "payment_status", "status")
	if status == "" {
		status = stringField(transaction, "payment_status", "status")
	}

	orderID := stringField(data, "order_id")
	if orderID == "" {
		orderID = providerOrderID
	}

	return provider.ProviderStatus{
		OrderID:        orderID,
		TransactionID:  stringField(data, "transid", "transaction_id"),
		Reference:      stringField(data, "reference"),
		Amount:         stringField(data, "amount"),
		Currency:       stringField(data, "currency"),
		Phone:          stringField(data, "msisdn", "phone"),
		Status:         NormalizeStatus(status),
		ProviderStatus: status,
		Raw:            parsed,
	}, nil
}

// Disburse and CheckPayoutStatus implement provider.Disburser.
//
// UNVERIFIED CONTRACT: docs.sonicpesa.com returned HTTP 403 to automated
// fetch, and no disbursement/B2C API is documented anywhere in this repo —
// only the collection endpoints above (/payment/create_order,
// /payment/order_status) have ever been confirmed. The endpoint paths and
// field names below are best-effort guesses modeled on SonicPesa's own
// collection-API conventions (same host, same X-API-KEY header, same
// {status, data} envelope). Confirm the real contract against SonicPesa's
// dashboard/docs before enabling PAYMENTS_AUTOMATED_PAYOUTS_ENABLED in
// production — until then this code path is never reached.
func (p *Provider) Disburse(ctx context.Context, req provider.DisburseRequest) (provider.DisburseResult, error) {
	payload := map[string]any{
		"amount":              json.Number(req.Amount),
		"currency":            req.Currency,
		"destination_type":    req.DestinationType,
		"destination_details": req.DestinationDetails,
		"reference":           req.ExternalReference,
	}

	parsed, err := p.post(ctx, "/payout/create", payload)
	if err != nil {
		return provider.DisburseResult{}, err
	}

	data := objectField(parsed, "data")
	if len(data) == 0 {
		data = parsed
	}

	payoutID := stringField(data, "payout_id", "disbursement_id", "order_id")
	if payoutID == "" {
		return provider.DisburseResult{}, errors.New("sonicpesa payout response missing payout_id")
	}

	status := stringField(data, "payout_status", "status")

	return provider.DisburseResult{
		ProviderPayoutID: payoutID,
		Status:           NormalizeStatus(status),
		ProviderStatus:   status,
		Raw:              parsed,
	}, nil
}

func (p *Provider) CheckPayoutStatus(ctx context.Context, providerPayoutID string) (provider.DisburseResult, error) {
	providerPayoutID = strings.TrimSpace(providerPayoutID)
	if providerPayoutID == "" {
		return provider.DisburseResult{}, errors.New("sonicpesa payout id is required")
	}

	parsed, err := p.post(ctx, "/payout/status", map[string]any{"payout_id": providerPayoutID})
	if err != nil {
		return provider.DisburseResult{}, err
	}

	data := objectField(parsed, "data")
	if len(data) == 0 {
		data = parsed
	}

	status := stringField(data, "payout_status", "status")
	payoutID := stringField(data, "payout_id", "disbursement_id")
	if payoutID == "" {
		payoutID = providerPayoutID
	}

	return provider.DisburseResult{
		ProviderPayoutID: payoutID,
		Status:           NormalizeStatus(status),
		ProviderStatus:   status,
		Raw:              parsed,
	}, nil
}

func NormalizeStatus(status string) provider.Status {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PENDING":
		return provider.StatusPending
	case "INPROGRESS", "IN_PROGRESS", "PROCESSING":
		return provider.StatusProcessing
	case "SUCCESS", "SUCCEEDED", "COMPLETED", "COMPLETE", "PAID":
		return provider.StatusPaid
	case "CANCELLED", "CANCELED", "USERCANCELLED", "USERCANCELED":
		return provider.StatusCancelled
	case "REJECTED", "FAILED", "FAIL":
		return provider.StatusFailed
	case "EXPIRED":
		return provider.StatusExpired
	case "REVERSED", "REFUNDED":
		return provider.StatusReversed
	default:
		return provider.StatusUnknown
	}
}

func normalizeSignature(signature string) string {
	signature = strings.TrimSpace(strings.ToLower(signature))
	signature = strings.TrimPrefix(signature, "sha256=")
	signature = strings.TrimPrefix(signature, "hmac-sha256=")
	return signature
}

func (p *Provider) post(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	apiKey := strings.TrimSpace(p.cfg.APIKey)
	if apiKey == "" {
		return nil, ErrAPIKeyMissing
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal sonicpesa request: %w", err)
	}

	url := strings.TrimRight(defaultString(p.cfg.BaseURL, "https://api.sonicpesa.com/api/v1"), "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build sonicpesa request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call sonicpesa: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sonicpesa error %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	var parsed map[string]any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode sonicpesa response: %w", err)
	}

	topStatus := strings.ToLower(stringField(parsed, "status"))
	if topStatus != "" && topStatus != "success" {
		message := stringField(parsed, "message", "error")
		if message == "" {
			message = "request failed"
		}
		return nil, fmt.Errorf("sonicpesa %s: %s", topStatus, message)
	}

	return parsed, nil
}

func stringField(payload map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := payload[name]; ok {
			if out := stringify(value); out != "" {
				return out
			}
		}
	}

	for _, nestedKey := range []string{"data", "transaction", "order_status_data"} {
		nested, ok := payload[nestedKey].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range names {
			if value, ok := nested[name]; ok {
				if out := stringify(value); out != "" {
					return out
				}
			}
		}
	}

	return ""
}

func objectField(payload map[string]any, name string) map[string]any {
	value, ok := payload[name]
	if !ok {
		return nil
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func parseTimePtr(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000000Z",
		"2006-01-02T15:04:05.000000-07:00",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}

	return nil
}
