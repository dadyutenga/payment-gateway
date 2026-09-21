package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"
	azcrypto "azsubay-payments-gateway/internal/platform/crypto"
	"azsubay-payments-gateway/internal/shared/audit"
	"azsubay-payments-gateway/internal/shared/notify"
	"azsubay-payments-gateway/internal/shared/validation"

	"github.com/google/uuid"
)

var (
	ErrUnknownProvider               = errors.New("unknown payment provider")
	ErrNoActiveProviderAccount       = errors.New("no active payment provider account configured")
	ErrPaymentAppUnauthorized        = errors.New("payment app unauthorized")
	ErrProviderOrderMissing          = errors.New("payment order missing provider order id")
	ErrWebhookVerificationFailed     = errors.New("payment webhook verification failed")
	ErrWebhookParseFailed            = errors.New("payment webhook parse failed")
	ErrPaymentEventPersistenceFailed = errors.New("payment event persistence failed")
)

type Service struct {
	repo                           Repository
	registry                       map[string]provider.Constructor
	cipher                         *azcrypto.Cipher
	deliverySigningSecret          string
	deliveryMaxAttempts            int
	reconciliationStaleAfter       time.Duration
	reconciliationBatchSize        int
	automatedPayoutsEnabled        bool
	payoutProvider                 string
	payoutReconciliationStaleAfter time.Duration
	http                           *http.Client
	log                            *slog.Logger
	notifier                       notify.Writer
	gate                           notify.NotificationGate
	audit                          audit.Writer
	sms                            SMSSender
}

// SMSSender is satisfied by sms.Service. Declared narrowly here (rather than
// importing the sms package directly) so this module stays decoupled — the
// same pattern reminders.Service uses for its own SMS dependency.
type SMSSender interface {
	SendSMS(ctx context.Context, appID, phone, message string) error
}

// SetNotifier wires the admin alert feed in. Optional — if never called,
// notification writes are silently skipped.
func (s *Service) SetNotifier(w notify.Writer) {
	s.notifier = w
}

// SetSMSSender wires payment-success SMS delivery to app members. Optional
// — if never called, sendPaymentSuccessSMS is a no-op (e.g. in tests).
func (s *Service) SetSMSSender(sms SMSSender) {
	s.sms = sms
}

// SetNotificationGate wires an optional per-event-type mute check. Optional
// — if never called, or if the gate errors, notifications fail open.
func (s *Service) SetNotificationGate(g notify.NotificationGate) {
	s.gate = g
}

// SetAuditWriter wires an activity-log sink so provider credential changes
// record which admin performed them. Optional — nil is a valid no-op state.
func (s *Service) SetAuditWriter(w audit.Writer) {
	s.audit = w
}

func (s *Service) logProviderAction(ctx context.Context, callerID, callerEmail, action, targetID string, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	_ = s.audit.LogAdminAction(ctx, callerID, callerEmail, action, "payment_provider_account", targetID, metadata)
}

// notifyAllowed fails open (true) if no gate is wired or the gate errors —
// a settings lookup failure must never silently swallow a real alert.
func (s *Service) notifyAllowed(ctx context.Context, eventType string) bool {
	if s.gate == nil {
		return true
	}
	allowed, err := s.gate.ShouldNotify(ctx, eventType)
	if err != nil {
		return true
	}
	return allowed
}

type ServiceOptions struct {
	DeliverySigningSecret    string
	DeliveryTimeout          time.Duration
	DeliveryMaxAttempts      int
	ReconciliationStaleAfter time.Duration
	ReconciliationBatchSize  int
	AutomatedPayoutsEnabled  bool
	// PayoutProvider is which registered provider kind AttemptPayout
	// disburses through. Defaults to "sonicpesa" — the only kind
	// implemented today — but is configurable so a future second
	// payout-capable provider doesn't require a code change to switch to.
	PayoutProvider                 string
	PayoutReconciliationStaleAfter time.Duration
	HTTPClient                     *http.Client
}

func NewService(repo Repository, registry map[string]provider.Constructor, cipher *azcrypto.Cipher, opts ServiceOptions, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.DeliverySigningSecret == "" {
		opts.DeliverySigningSecret = "development-payment-delivery-secret"
	}
	if opts.DeliveryMaxAttempts <= 0 {
		opts.DeliveryMaxAttempts = 10
	}
	if opts.DeliveryTimeout <= 0 {
		opts.DeliveryTimeout = 10 * time.Second
	}
	if opts.ReconciliationStaleAfter <= 0 {
		opts.ReconciliationStaleAfter = 15 * time.Minute
	}
	if opts.ReconciliationBatchSize <= 0 {
		opts.ReconciliationBatchSize = 50
	}
	if opts.PayoutReconciliationStaleAfter <= 0 {
		opts.PayoutReconciliationStaleAfter = 15 * time.Minute
	}
	if opts.PayoutProvider == "" {
		opts.PayoutProvider = "sonicpesa"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.DeliveryTimeout}
	}

	return &Service{
		repo:                           repo,
		registry:                       registry,
		cipher:                         cipher,
		deliverySigningSecret:          opts.DeliverySigningSecret,
		deliveryMaxAttempts:            opts.DeliveryMaxAttempts,
		reconciliationStaleAfter:       opts.ReconciliationStaleAfter,
		reconciliationBatchSize:        opts.ReconciliationBatchSize,
		automatedPayoutsEnabled:        opts.AutomatedPayoutsEnabled,
		payoutProvider:                 opts.PayoutProvider,
		payoutReconciliationStaleAfter: opts.PayoutReconciliationStaleAfter,
		http:                           opts.HTTPClient,
		log:                            logger,
	}
}

// resolveProvider loads the active default app.payment_provider_accounts row
// for the given provider kind, decrypts its credentials, and constructs a
// live adapter instance via the registry. Mirrors sms.Service's
// resolveProviderAndSenderID — same fail-with-clear-error posture when no
// active default account is configured for that kind.
func (s *Service) resolveProvider(ctx context.Context, kind string) (provider.PaymentProvider, error) {
	kind = normalizeProviderName(kind)
	constructor, ok := s.registry[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, kind)
	}

	account, err := s.repo.GetDefaultProviderAccount(ctx, kind)
	if errors.Is(err, ErrPaymentProviderAccountNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNoActiveProviderAccount, kind)
	}
	if err != nil {
		return nil, err
	}

	decrypted, err := s.cipher.Decrypt(account.CredentialsEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt payment provider credentials: %w", err)
	}
	var creds map[string]string
	if err := json.Unmarshal([]byte(decrypted), &creds); err != nil {
		return nil, fmt.Errorf("parse payment provider credentials: %w", err)
	}

	return constructor(account.BaseURL, creds), nil
}

func (s *Service) ListProviderAccounts(ctx context.Context) ([]PaymentProviderAccount, error) {
	return s.repo.ListProviderAccounts(ctx)
}

func (s *Service) CreateProviderAccount(ctx context.Context, callerID, callerEmail string, input CreateProviderAccountInput) (PaymentProviderAccount, validation.Errors, error) {
	input.Provider = normalizeProviderName(input.Provider)
	input.Name = strings.TrimSpace(input.Name)
	input.Environment = defaultString(strings.ToLower(strings.TrimSpace(input.Environment)), "production")
	input.BaseURL = strings.TrimSpace(input.BaseURL)

	errs := validation.Errors{}
	validation.Required(input.Provider, "Provider is required.", errs, "provider")
	validation.Required(input.Name, "Name is required.", errs, "name")
	if _, ok := s.registry[input.Provider]; input.Provider != "" && !ok {
		errs.Add("provider", "This provider kind is not implemented yet.")
	}
	if len(input.Credentials) == 0 {
		errs.Add("credentials", "Credentials are required.")
	}
	if errs.Any() {
		return PaymentProviderAccount{}, errs, nil
	}

	payload, err := json.Marshal(input.Credentials)
	if err != nil {
		return PaymentProviderAccount{}, nil, fmt.Errorf("marshal payment provider credentials: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(string(payload))
	if err != nil {
		return PaymentProviderAccount{}, nil, fmt.Errorf("encrypt payment provider credentials: %w", err)
	}

	account, err := s.repo.CreateProviderAccount(ctx, input.Provider, input.Name, input.Environment, input.BaseURL, encrypted)
	if err != nil {
		return PaymentProviderAccount{}, nil, err
	}
	s.logProviderAction(ctx, callerID, callerEmail, "payment_provider.created", account.ID, map[string]any{"provider": account.Provider, "name": account.Name, "environment": account.Environment})
	return account, nil, nil
}

func (s *Service) UpdateProviderAccount(ctx context.Context, callerID, callerEmail, id string, input UpdateProviderAccountInput) (PaymentProviderAccount, validation.Errors, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Environment = defaultString(strings.ToLower(strings.TrimSpace(input.Environment)), "production")
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Status = defaultString(strings.ToLower(strings.TrimSpace(input.Status)), "active")

	errs := validation.Errors{}
	validation.Required(input.Name, "Name is required.", errs, "name")
	if errs.Any() {
		return PaymentProviderAccount{}, errs, nil
	}

	credentialsRotated := len(input.Credentials) > 0
	var encryptedPtr *string
	if credentialsRotated {
		payload, err := json.Marshal(input.Credentials)
		if err != nil {
			return PaymentProviderAccount{}, nil, fmt.Errorf("marshal payment provider credentials: %w", err)
		}
		encrypted, err := s.cipher.Encrypt(string(payload))
		if err != nil {
			return PaymentProviderAccount{}, nil, fmt.Errorf("encrypt payment provider credentials: %w", err)
		}
		encryptedPtr = &encrypted
	}

	account, err := s.repo.UpdateProviderAccount(ctx, id, input.Name, input.Environment, input.BaseURL, input.Status, encryptedPtr)
	if err != nil {
		return PaymentProviderAccount{}, nil, err
	}
	s.logProviderAction(ctx, callerID, callerEmail, "payment_provider.updated", id, map[string]any{"name": input.Name, "credentials_rotated": credentialsRotated, "status": input.Status})
	return account, nil, nil
}

func (s *Service) DeleteProviderAccount(ctx context.Context, callerID, callerEmail, id string) error {
	if err := s.repo.DeleteProviderAccount(ctx, id); err != nil {
		return err
	}
	s.logProviderAction(ctx, callerID, callerEmail, "payment_provider.deleted", id, nil)
	return nil
}

func (s *Service) SetDefaultProviderAccount(ctx context.Context, callerID, callerEmail, id string) error {
	if err := s.repo.SetDefaultProviderAccount(ctx, id); err != nil {
		return err
	}
	s.logProviderAction(ctx, callerID, callerEmail, "payment_provider.set_default", id, nil)
	return nil
}

func (s *Service) AuthenticateApp(ctx context.Context, rawAPIKey string) (PaymentApp, error) {
	rawAPIKey = strings.TrimSpace(rawAPIKey)
	if rawAPIKey == "" {
		return PaymentApp{}, ErrPaymentAppUnauthorized
	}

	app, err := s.repo.GetAppByAPIKeyHash(ctx, hashAPIKey(rawAPIKey))
	if errors.Is(err, ErrPaymentAppNotFound) {
		return PaymentApp{}, ErrPaymentAppUnauthorized
	}
	if err != nil {
		return PaymentApp{}, err
	}
	return app, nil
}

func (s *Service) CreateApp(ctx context.Context, input CreatePaymentAppInput) (CreatePaymentAppResult, validation.Errors, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)

	errs := validation.Errors{}
	validation.Required(input.Name, "Name is required.", errs, "name")
	validation.MaxRunes(input.Name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.MaxRunes(input.Description, 500, "Description must be 500 characters or fewer.", errs, "description")
	if errs.Any() {
		return CreatePaymentAppResult{}, errs, nil
	}

	rawKey, err := randomHex(32)
	if err != nil {
		return CreatePaymentAppResult{}, nil, err
	}

	app, err := s.repo.CreatePaymentApp(ctx, input)
	if err != nil {
		return CreatePaymentAppResult{}, nil, err
	}
	if err := s.repo.CreatePaymentAPIKey(ctx, app.ID, hashAPIKey(rawKey)); err != nil {
		return CreatePaymentAppResult{}, nil, err
	}

	return CreatePaymentAppResult{App: app, APIKey: rawKey}, nil, nil
}

// GenerateAPIKey revokes any existing active key for the app and issues a
// new one, so an app only ever has at most one active key at a time. The
// raw key is returned exactly once and is never stored — only its hash is
// (mirrors products.Service.GenerateAPIKey).
func (s *Service) GenerateAPIKey(ctx context.Context, appID string) (string, error) {
	rawKey, err := randomHex(32)
	if err != nil {
		return "", err
	}

	if err := s.repo.RevokePaymentAPIKeys(ctx, appID); err != nil {
		return "", err
	}
	if err := s.repo.CreatePaymentAPIKey(ctx, appID, hashAPIKey(rawKey)); err != nil {
		return "", err
	}

	return rawKey, nil
}

func (s *Service) ListApps(ctx context.Context, limit, offset int) (PaymentAppListResult, error) {
	limit, offset = boundedLimitOffset(limit, offset, 50, 200)
	return s.repo.ListPaymentApps(ctx, limit, offset)
}

// ---------- Ledger, balances, fees ----------

func (s *Service) UpdateAppFees(ctx context.Context, appID string, input UpdateAppFeesInput) (PaymentApp, validation.Errors, error) {
	errs := validation.Errors{}
	if input.FeeType != "fixed" && input.FeeType != "percentage" && input.FeeType != "hybrid" {
		errs.Add("fee_type", "Fee type must be fixed, percentage, or hybrid.")
	}
	if _, ok := new(big.Rat).SetString(input.FeePercent); !ok {
		errs.Add("fee_percent", "Fee percent must be a number.")
	}
	if _, ok := new(big.Rat).SetString(input.FeeFixed); !ok {
		errs.Add("fee_fixed", "Fee fixed amount must be a number.")
	}
	if errs.Any() {
		return PaymentApp{}, errs, nil
	}

	app, err := s.repo.UpdateAppFees(ctx, appID, input)
	return app, nil, err
}

// DeletePaymentApp soft-deletes (see the repo method's doc comment for why
// a real DELETE is never acceptable here) and audit-logs the action, since
// removing an app from active use is a security/financial-relevant admin
// action the same way provider-account changes already are.
func (s *Service) DeletePaymentApp(ctx context.Context, callerID, callerEmail, appID string) (PaymentApp, error) {
	app, err := s.repo.DeletePaymentApp(ctx, appID)
	if err != nil {
		return PaymentApp{}, err
	}
	if s.audit != nil {
		_ = s.audit.LogAdminAction(ctx, callerID, callerEmail, "delete", "payment_app", appID, map[string]any{"name": app.Name})
	}
	return app, nil
}

func (s *Service) GetAppBalance(ctx context.Context, appID string) (AppBalance, error) {
	return s.repo.GetAppBalance(ctx, appID)
}

func (s *Service) ListLedgerEntries(ctx context.Context, filter LedgerEntryListFilter) (LedgerEntryListResult, error) {
	return s.repo.ListLedgerEntries(ctx, filter)
}

// ---------- Withdrawals ----------
// Admin-recorded on behalf of an app for now — there's no merchant
// self-service login yet (see PAYMENTS_GATEWAY_ARCHITECTURE.md). The hard
// balance guarantee lives in the repository's ApproveWithdrawal (locked
// transaction); this pre-check only exists to fail fast with a clear error
// at request time instead of waiting until approval.
func (s *Service) CreateWithdrawal(ctx context.Context, requestedBy string, input CreateWithdrawalInput) (PaymentWithdrawal, validation.Errors, error) {
	input.Notes = strings.TrimSpace(input.Notes)
	input.RequestedBy = requestedBy

	errs := validation.Errors{}
	validation.Required(input.AppID, "App is required.", errs, "app_id")
	if input.DestinationType != "bank" && input.DestinationType != "mobile_money" {
		errs.Add("destination_type", "Destination type must be bank or mobile_money.")
	}

	amount, amountErr := normalizeAmount(input.Amount)
	if amountErr != nil {
		errs.Add("amount", "Amount must be a positive number.")
	}
	input.Amount = amount
	if input.Currency == "" {
		input.Currency = "TZS"
	}
	if errs.Any() {
		return PaymentWithdrawal{}, errs, nil
	}

	balance, err := s.repo.GetAppBalance(ctx, input.AppID)
	if err != nil {
		return PaymentWithdrawal{}, nil, fmt.Errorf("load app balance: %w", err)
	}
	availableRat, _ := new(big.Rat).SetString(balance.AvailableBalance)
	amountRat, _ := new(big.Rat).SetString(input.Amount)
	if availableRat == nil || amountRat == nil || amountRat.Cmp(availableRat) > 0 {
		return PaymentWithdrawal{}, nil, ErrInsufficientBalance
	}

	withdrawal, err := s.repo.CreateWithdrawal(ctx, input)
	return withdrawal, nil, err
}

func (s *Service) ListWithdrawals(ctx context.Context, filter WithdrawalListFilter) (WithdrawalListResult, error) {
	return s.repo.ListWithdrawals(ctx, filter)
}

func (s *Service) GetWithdrawal(ctx context.Context, id string) (PaymentWithdrawal, error) {
	return s.repo.GetWithdrawalByID(ctx, id)
}

// ApproveWithdrawal debits the ledger via the repository as before, then —
// only if PAYMENTS_AUTOMATED_PAYOUTS_ENABLED is on — attempts to dispatch
// the payout immediately. The dispatch attempt happens after the approval
// transaction has already committed, since it makes a network call and
// must never run inside the DB transaction holding the withdrawal row
// lock. A dispatch failure here is logged, not returned as an error —
// approval already succeeded and is safely recoverable via retry or the
// manual mark-paid/mark-failed fallback.
func (s *Service) ApproveWithdrawal(ctx context.Context, id, approvedBy string) (PaymentWithdrawal, error) {
	withdrawal, err := s.repo.ApproveWithdrawal(ctx, id, approvedBy)
	if err != nil {
		return PaymentWithdrawal{}, err
	}
	if !s.automatedPayoutsEnabled {
		return withdrawal, nil
	}
	dispatched, attemptErr := s.AttemptPayout(ctx, withdrawal.ID)
	if attemptErr != nil {
		s.log.Error("automated payout attempt failed", "withdrawal_id", withdrawal.ID, "error", attemptErr)
		return withdrawal, nil
	}
	return dispatched, nil
}

func (s *Service) RejectWithdrawal(ctx context.Context, id string) (PaymentWithdrawal, error) {
	return s.repo.RejectWithdrawal(ctx, id)
}

func (s *Service) MarkWithdrawalPaid(ctx context.Context, id string) (PaymentWithdrawal, error) {
	return s.repo.MarkWithdrawalPaid(ctx, id)
}

func (s *Service) MarkWithdrawalFailed(ctx context.Context, id, notes string) (PaymentWithdrawal, error) {
	return s.repo.MarkWithdrawalFailed(ctx, id, notes)
}

// AttemptPayout resolves the withdrawal's payout provider (SonicPesa today)
// and requests a disbursement. Safe to call repeatedly: DispatchWithdrawal
// only ever succeeds once per withdrawal (guarded by provider_payout_id IS
// NULL plus the unique index from migration 000027), so retrying after a
// successful dispatch just returns ErrWithdrawalAlreadyDispatched rather
// than risking a second payout.
//
// A Disburse call error deliberately never marks the withdrawal failed
// automatically — that would reverse a ledger debit for money that may
// already be in flight at the provider. It's recorded via
// RecordPayoutFailure instead, leaving the withdrawal "approved" with
// FailureReason set so an admin can retry or fall back to a manual
// mark-paid/mark-failed attestation.
func (s *Service) AttemptPayout(ctx context.Context, id string) (PaymentWithdrawal, error) {
	withdrawal, err := s.repo.GetWithdrawalByID(ctx, id)
	if err != nil {
		return PaymentWithdrawal{}, err
	}
	if withdrawal.Status != WithdrawalStatusApproved {
		return PaymentWithdrawal{}, ErrInvalidWithdrawalTransition
	}

	p, err := s.resolveProvider(ctx, s.payoutProvider)
	if err != nil {
		return PaymentWithdrawal{}, err
	}
	disburser, ok := p.(provider.Disburser)
	if !ok {
		return PaymentWithdrawal{}, ErrProviderDoesNotSupportPayouts
	}

	result, disburseErr := disburser.Disburse(ctx, provider.DisburseRequest{
		Amount:             withdrawal.Amount,
		Currency:           withdrawal.Currency,
		DestinationType:    withdrawal.DestinationType,
		DestinationDetails: withdrawal.DestinationDetails,
		ExternalReference:  withdrawal.ID,
	})
	if disburseErr != nil {
		if recorded, recErr := s.repo.RecordPayoutFailure(ctx, withdrawal.ID, s.payoutProvider, disburseErr.Error()); recErr == nil {
			return recorded, disburseErr
		}
		return PaymentWithdrawal{}, disburseErr
	}

	dispatched, err := s.repo.DispatchWithdrawal(ctx, withdrawal.ID, s.payoutProvider, result.ProviderPayoutID, WithdrawalStatusProcessing, result.ProviderStatus)
	if err != nil {
		return PaymentWithdrawal{}, err
	}

	switch result.Status {
	case provider.StatusPaid:
		return s.repo.MarkWithdrawalPaid(ctx, dispatched.ID)
	case provider.StatusFailed:
		return s.repo.MarkWithdrawalFailed(ctx, dispatched.ID, "Payout failed at provider: "+result.ProviderStatus)
	default:
		return dispatched, nil
	}
}

// ReconcilePayouts is the safety net for a payout webhook that never
// arrives — mirrors ReconcilePayments exactly, polling CheckPayoutStatus
// for withdrawals stuck in "processing" past payoutReconciliationStaleAfter.
func (s *Service) ReconcilePayouts(ctx context.Context, limit int) (ReconcilePayoutsResult, error) {
	if limit <= 0 {
		limit = s.reconciliationBatchSize
	}
	staleBefore := time.Now().UTC().Add(-s.payoutReconciliationStaleAfter)

	withdrawals, err := s.repo.ListPayoutReconciliationCandidates(ctx, staleBefore, limit)
	if err != nil {
		return ReconcilePayoutsResult{}, err
	}

	result := ReconcilePayoutsResult{Scanned: len(withdrawals)}
	for _, withdrawal := range withdrawals {
		p, err := s.resolveProvider(ctx, withdrawal.Provider)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", withdrawal.ID, err))
			continue
		}
		disburser, ok := p.(provider.Disburser)
		if !ok {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", withdrawal.ID, ErrProviderDoesNotSupportPayouts))
			continue
		}

		status, err := disburser.CheckPayoutStatus(ctx, withdrawal.ProviderPayoutID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", withdrawal.ID, err))
			continue
		}
		result.Checked++

		switch status.Status {
		case provider.StatusPaid:
			if _, err := s.repo.MarkWithdrawalPaid(ctx, withdrawal.ID); err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", withdrawal.ID, err))
				continue
			}
			result.Updated++
		case provider.StatusFailed, provider.StatusReversed:
			if _, err := s.repo.MarkWithdrawalFailed(ctx, withdrawal.ID, "Payout failed at provider: "+status.ProviderStatus); err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", withdrawal.ID, err))
				continue
			}
			result.Updated++
		default:
			result.Unchanged++
		}
	}

	if result.Scanned > 0 {
		s.log.Info(
			"payout reconciliation completed",
			"scanned", result.Scanned,
			"checked", result.Checked,
			"updated", result.Updated,
			"unchanged", result.Unchanged,
			"failed", result.Failed,
		)
	}

	return result, nil
}

// ---------- Merchant identity & access ----------

func (s *Service) AddAppMemberByEmail(ctx context.Context, appID, email, addedBy string) (AppMember, validation.Errors, error) {
	email = strings.TrimSpace(email)

	errs := validation.Errors{}
	validation.Required(email, "Email is required.", errs, "email")
	validation.ValidEmail(email, "Enter a valid email address.", errs, "email")
	if errs.Any() {
		return AppMember{}, errs, nil
	}

	userID, err := s.repo.FindUserIDByEmail(ctx, email)
	if err != nil {
		return AppMember{}, nil, err
	}

	member, err := s.repo.AddAppMember(ctx, appID, userID, addedBy)
	return member, nil, err
}

func (s *Service) ListAppMembers(ctx context.Context, appID string) ([]AppMember, error) {
	return s.repo.ListAppMembers(ctx, appID)
}

func (s *Service) RemoveAppMember(ctx context.Context, appID, userID string) error {
	return s.repo.RemoveAppMember(ctx, appID, userID)
}

func (s *Service) IsAppMember(ctx context.Context, userID, appID string) (bool, error) {
	return s.repo.IsAppMember(ctx, userID, appID)
}

func (s *Service) ListAppsForUser(ctx context.Context, userID string) ([]PaymentApp, error) {
	return s.repo.ListAppsForUser(ctx, userID)
}

// ---------- Refunds & reversals ----------
// Manual/admin fallback for when no provider webhook drives the automatic
// path in HandleProviderWebhook (PostRefund).

func (s *Service) RefundOrder(ctx context.Context, orderID string) (PaymentOrder, error) {
	return s.repo.RefundOrder(ctx, orderID, "admin")
}

func (s *Service) CreateOrder(ctx context.Context, app PaymentApp, input CreatePaymentOrderInput) (PaymentOrder, validation.Errors, error) {
	input = normalizeCreateOrderInput(input)

	errs := validation.Errors{}
	validation.Required(input.Provider, "Provider is required.", errs, "provider")
	validation.Required(input.Amount, "Amount is required.", errs, "amount")
	validation.Required(input.Currency, "Currency is required.", errs, "currency")
	validation.Required(input.BuyerName, "Buyer name is required.", errs, "buyer_name")
	validation.Required(input.BuyerEmail, "Buyer email is required.", errs, "buyer_email")
	validation.Required(input.BuyerPhone, "Buyer phone is required.", errs, "buyer_phone")
	validation.ValidEmail(input.BuyerEmail, "Buyer email must be valid.", errs, "buyer_email")
	validation.MaxRunes(input.Currency, 8, "Currency must be 8 characters or fewer.", errs, "currency")
	validation.MaxRunes(input.BuyerName, 120, "Buyer name must be 120 characters or fewer.", errs, "buyer_name")
	validation.MaxRunes(input.BuyerEmail, 255, "Buyer email must be 255 characters or fewer.", errs, "buyer_email")
	validation.MaxRunes(input.BuyerPhone, 32, "Buyer phone must be 32 characters or fewer.", errs, "buyer_phone")
	validation.MaxRunes(input.ExternalReference, 150, "External reference must be 150 characters or fewer.", errs, "external_reference")

	amount, amountErr := normalizeAmount(input.Amount)
	if amountErr != nil {
		errs.Add("amount", "Amount must be a positive number.")
	}
	input.Amount = amount

	p, providerErr := s.resolveProvider(ctx, input.Provider)
	if providerErr != nil {
		errs.Add("provider", "Payment provider is not supported or not configured.")
	}

	if errs.Any() {
		return PaymentOrder{}, errs, nil
	}

	if input.ExternalReference != "" {
		existing, err := s.repo.GetPaymentOrderByAppReference(ctx, app.ID, input.ExternalReference)
		if err == nil {
			return existing, nil, nil
		}
		if !errors.Is(err, ErrPaymentOrderNotFound) {
			return PaymentOrder{}, nil, err
		}
	}

	providerOrder, err := p.CreateOrder(ctx, provider.CreateOrderRequest{
		Amount:            input.Amount,
		Currency:          input.Currency,
		BuyerName:         input.BuyerName,
		BuyerEmail:        input.BuyerEmail,
		BuyerPhone:        input.BuyerPhone,
		ExternalReference: input.ExternalReference,
		Metadata:          input.Metadata,
	})
	if err != nil {
		return PaymentOrder{}, nil, fmt.Errorf("create provider payment order: %w", err)
	}

	status := providerOrder.Status
	if status == "" || status == provider.StatusUnknown {
		status = provider.StatusPending
	}

	metadata := copyMetadata(input.Metadata)
	if providerOrder.Reference != "" {
		metadata["provider_reference"] = providerOrder.Reference
	}
	if providerOrder.Raw != nil {
		metadata["provider_response"] = providerOrder.Raw
	}

	order, err := s.repo.CreatePaymentOrder(ctx, CreatePaymentOrderRepositoryInput{
		AppID:                 app.ID,
		Provider:              input.Provider,
		ProviderOrderID:       providerOrder.OrderID,
		ProviderTransactionID: providerOrder.TransactionID,
		ExternalReference:     input.ExternalReference,
		Amount:                input.Amount,
		Currency:              input.Currency,
		BuyerName:             input.BuyerName,
		BuyerEmail:            input.BuyerEmail,
		BuyerPhone:            input.BuyerPhone,
		Status:                status,
		ProviderStatus:        providerOrder.ProviderStatus,
		Metadata:              metadata,
	})
	if err != nil {
		return PaymentOrder{}, nil, err
	}

	return order, nil, nil
}

// autoRefreshStaleAfter bounds how long GetOrder will trust a non-final
// stored status before quietly forcing a live provider check on read. This
// is the safety net for "stuck on pending" when the provider's webhook
// never arrives at all (nothing to redeliver — see deliverDueNow, which
// only helps once an event exists) and nobody has called Refresh: any app
// that polls GetOrder after a payment (the pattern this backend's own docs
// recommend) self-heals within one poll interval even in that case.
const autoRefreshStaleAfter = 20 * time.Second

func (s *Service) GetOrder(ctx context.Context, app PaymentApp, paymentOrderID string) (PaymentOrder, error) {
	order, err := s.repo.GetPaymentOrderByIDForApp(ctx, app.ID, strings.TrimSpace(paymentOrderID))
	if err != nil {
		return PaymentOrder{}, err
	}

	if isNonFinalStatus(order.Status) &&
		strings.TrimSpace(order.ProviderOrderID) != "" &&
		time.Since(order.UpdatedAt) > autoRefreshStaleAfter {
		if refreshed, refreshErr := s.refreshOrder(ctx, order); refreshErr == nil {
			return refreshed, nil
		}
		// Best-effort: a failed live check (provider hiccup, timeout) must
		// never break a plain status read — fall back to the stored order.
	}

	return order, nil
}

func (s *Service) RefreshOrder(ctx context.Context, app PaymentApp, paymentOrderID string) (PaymentOrder, error) {
	order, err := s.repo.GetPaymentOrderByIDForApp(ctx, app.ID, strings.TrimSpace(paymentOrderID))
	if err != nil {
		return PaymentOrder{}, err
	}
	return s.refreshOrder(ctx, order)
}

// refreshOrder does the actual live provider check + status apply for an
// already-fetched order. Shared by RefreshOrder (explicit, app-triggered)
// and GetOrder's auto-refresh (implicit, staleness-triggered) so there's
// exactly one place that talks to the provider for a status check.
func (s *Service) refreshOrder(ctx context.Context, order PaymentOrder) (PaymentOrder, error) {
	if strings.TrimSpace(order.ProviderOrderID) == "" {
		return PaymentOrder{}, ErrProviderOrderMissing
	}

	p, err := s.resolveProvider(ctx, order.Provider)
	if err != nil {
		return PaymentOrder{}, err
	}

	providerStatus, err := p.CheckOrderStatus(ctx, order.ProviderOrderID)
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("check provider payment order status: %w", err)
	}

	updated, _, _, err := s.applyProviderStatusUpdate(ctx, order, providerStatus, "refresh")
	if err != nil {
		return PaymentOrder{}, err
	}
	return updated, nil
}

func (s *Service) ReconcilePayments(ctx context.Context, limit int) (ReconcilePaymentsResult, error) {
	if limit <= 0 {
		limit = s.reconciliationBatchSize
	}
	staleBefore := time.Now().UTC().Add(-s.reconciliationStaleAfter)

	orders, err := s.repo.ListReconciliationCandidates(ctx, staleBefore, limit)
	if err != nil {
		return ReconcilePaymentsResult{}, err
	}

	result := ReconcilePaymentsResult{Scanned: len(orders)}
	for _, order := range orders {
		if strings.TrimSpace(order.ProviderOrderID) == "" {
			result.Unchanged++
			continue
		}

		p, err := s.resolveProvider(ctx, order.Provider)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", order.ID, err))
			continue
		}

		providerStatus, err := p.CheckOrderStatus(ctx, order.ProviderOrderID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", order.ID, err))
			continue
		}
		result.Checked++

		updated, _, deliveries, err := s.applyProviderStatusUpdate(ctx, order, providerStatus, "reconciliation")
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", order.ID, err))
			continue
		}
		result.DeliveriesCreated += deliveries
		if updated.Status != order.Status {
			result.Updated++
		} else {
			result.Unchanged++
		}
	}

	if result.Scanned > 0 {
		s.log.Info(
			"payment reconciliation completed",
			"scanned", result.Scanned,
			"checked", result.Checked,
			"updated", result.Updated,
			"unchanged", result.Unchanged,
			"failed", result.Failed,
			"deliveries_created", result.DeliveriesCreated,
		)
	}

	return result, nil
}

func (s *Service) SearchPaymentOrders(ctx context.Context, filter PaymentOrderSearchFilter) (PaymentOrderSearchResult, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.AppID = strings.TrimSpace(filter.AppID)
	filter.Provider = normalizeProviderName(filter.Provider)
	filter.Phone = strings.TrimSpace(filter.Phone)
	filter.PaymentID = strings.TrimSpace(filter.PaymentID)
	filter.ProviderOrderID = strings.TrimSpace(filter.ProviderOrderID)
	filter.ExternalReference = strings.TrimSpace(filter.ExternalReference)
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)
	return s.repo.SearchPaymentOrders(ctx, filter)
}

func (s *Service) ListPaymentEvents(ctx context.Context, filter PaymentEventListFilter) (PaymentEventListResult, error) {
	filter.Provider = normalizeProviderName(filter.Provider)
	filter.EventType = strings.TrimSpace(filter.EventType)
	filter.PaymentOrderID = strings.TrimSpace(filter.PaymentOrderID)
	filter.ProviderOrderID = strings.TrimSpace(filter.ProviderOrderID)
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)
	return s.repo.ListPaymentEvents(ctx, filter)
}

func (s *Service) ListWebhookDeliveries(ctx context.Context, filter PaymentWebhookDeliveryListFilter) (PaymentWebhookDeliveryListResult, error) {
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	filter.EventID = strings.TrimSpace(filter.EventID)
	filter.EndpointID = strings.TrimSpace(filter.EndpointID)
	filter.PaymentOrderID = strings.TrimSpace(filter.PaymentOrderID)
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)
	return s.repo.ListWebhookDeliveries(ctx, filter)
}

func (s *Service) ReplayFailedDelivery(ctx context.Context, deliveryID string) error {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return errors.New("delivery id is required")
	}
	if err := s.repo.ReplayWebhookDelivery(ctx, deliveryID); err != nil {
		return err
	}
	s.log.Info("payment webhook delivery replay queued", "delivery_id", deliveryID)
	return nil
}

func (s *Service) PaymentMetrics(ctx context.Context) (PaymentMetrics, error) {
	staleBefore := time.Now().UTC().Add(-s.reconciliationStaleAfter)
	return s.repo.GetPaymentMetrics(ctx, staleBefore)
}

func (s *Service) CreateWebhookEndpoint(ctx context.Context, input CreatePaymentWebhookEndpointInput) (CreatePaymentWebhookEndpointResult, validation.Errors, error) {
	input.AppID = strings.TrimSpace(input.AppID)
	input.URL = strings.TrimSpace(input.URL)
	input.EventTypes = normalizeEventTypes(input.EventTypes)

	errs := validation.Errors{}
	validation.Required(input.AppID, "App ID is required.", errs, "app_id")
	validation.Required(input.URL, "Webhook URL is required.", errs, "url")
	if err := validateWebhookURL(input.URL); err != nil {
		errs.Add("url", "Webhook URL must be a valid http or https URL.")
	}
	if len(input.EventTypes) == 0 {
		input.EventTypes = []string{"payment.updated"}
	}
	for _, eventType := range input.EventTypes {
		validation.MaxRunes(eventType, 80, "Event type must be 80 characters or fewer.", errs, "event_types")
	}
	if errs.Any() {
		return CreatePaymentWebhookEndpointResult{}, errs, nil
	}

	endpointID := uuid.NewString()
	signingSecret := s.endpointSigningSecret(endpointID)
	endpoint, err := s.repo.CreatePaymentWebhookEndpoint(ctx, CreatePaymentWebhookEndpointRepositoryInput{
		ID:         endpointID,
		AppID:      input.AppID,
		URL:        input.URL,
		EventTypes: input.EventTypes,
		SecretHash: hashAPIKey(signingSecret),
	})
	if err != nil {
		return CreatePaymentWebhookEndpointResult{}, nil, err
	}

	return CreatePaymentWebhookEndpointResult{
		Endpoint:      endpoint,
		SigningSecret: signingSecret,
	}, nil, nil
}

func (s *Service) ListWebhookEndpoints(ctx context.Context, appID string) ([]PaymentWebhookEndpoint, error) {
	return s.repo.ListWebhookEndpoints(ctx, strings.TrimSpace(appID))
}

func (s *Service) ReplayEvent(ctx context.Context, eventID string) (ReplayPaymentEventResult, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ReplayPaymentEventResult{}, errors.New("event id is required")
	}
	created, err := s.repo.CreateWebhookDeliveriesForEvent(ctx, eventID, "payment.updated")
	if err != nil {
		return ReplayPaymentEventResult{}, err
	}
	replayed, err := s.repo.ReplayFailedWebhookDeliveriesForEvent(ctx, eventID)
	if err != nil {
		return ReplayPaymentEventResult{}, err
	}
	if created+replayed > 0 {
		go s.deliverDueNow()
	}
	result := ReplayPaymentEventResult{
		CreatedDeliveries:  created,
		ReplayedDeliveries: replayed,
		QueuedDeliveries:   created + replayed,
	}
	s.log.Info(
		"payment event replay queued",
		"event_id", eventID,
		"created_deliveries", result.CreatedDeliveries,
		"replayed_deliveries", result.ReplayedDeliveries,
		"queued_deliveries", result.QueuedDeliveries,
	)
	return result, nil
}

// deliverDueNow makes one best-effort, immediate delivery pass, detached
// from any request context (which would be cancelled the moment the calling
// HTTP handler returns). Errors are logged, never propagated — this is
// purely a latency optimization on top of cmd/worker's own poll loop, which
// still owns retries and is the backstop if this attempt itself fails or
// the process restarts mid-flight.
func (s *Service) deliverDueNow() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if _, err := s.ProcessDueDeliveries(ctx, 25); err != nil {
		s.log.Error("inline payment webhook delivery attempt failed", "error", err)
	}
}

func (s *Service) ProcessDueDeliveries(ctx context.Context, limit int) (ProcessDeliveriesResult, error) {
	jobs, err := s.repo.ClaimDueWebhookDeliveries(ctx, limit)
	if err != nil {
		return ProcessDeliveriesResult{}, err
	}

	result := ProcessDeliveriesResult{Claimed: len(jobs)}
	for _, job := range jobs {
		responseStatus, deliveryErr := s.deliverWebhook(ctx, job)
		success := deliveryErr == nil && responseStatus >= 200 && responseStatus < 300

		record := RecordWebhookDeliveryAttemptInput{
			DeliveryID:     job.ID,
			Success:        success,
			ResponseStatus: responseStatus,
		}
		switch {
		case success:
			result.Delivered++
		case job.AttemptCount >= s.deliveryMaxAttempts:
			result.Failed++
			record.Failed = true
			record.Error = deliveryErrorMessage(deliveryErr, responseStatus)
		default:
			result.Retrying++
			nextAttempt := time.Now().UTC().Add(deliveryBackoff(job.AttemptCount))
			record.NextAttemptAt = &nextAttempt
			record.Error = deliveryErrorMessage(deliveryErr, responseStatus)
		}

		if err := s.repo.RecordWebhookDeliveryAttempt(ctx, record); err != nil {
			return result, err
		}
	}

	if result.Claimed > 0 {
		s.log.Info(
			"payment webhook deliveries processed",
			"claimed", result.Claimed,
			"delivered", result.Delivered,
			"retrying", result.Retrying,
			"failed", result.Failed,
		)
	}

	return result, nil
}

func (s *Service) HandleProviderWebhook(ctx context.Context, providerName string, headers http.Header, rawBody []byte) (WebhookResult, error) {
	name := normalizeProviderName(providerName)
	p, err := s.resolveProvider(ctx, name)
	if err != nil {
		return WebhookResult{}, err
	}

	payloadHash := sha256Hex(rawBody)
	if err := p.VerifyWebhook(headers, rawBody); err != nil {
		event, storeErr := s.storeWebhookEvent(ctx, PaymentEventInput{
			Provider:       name,
			EventType:      "webhook.signature_invalid",
			SignatureValid: false,
			PayloadHash:    payloadHash,
			DedupeKey:      dedupeKey(name, "invalid", "", "", payloadHash),
			Headers:        headers,
			RawBody:        string(rawBody),
			Error:          err.Error(),
		})
		if storeErr != nil {
			s.log.Error("store invalid payment webhook failed", "provider", name, "error", storeErr)
		}
		return WebhookResult{EventID: event.ID, Duplicate: event.Duplicate}, fmt.Errorf("%w: %v", ErrWebhookVerificationFailed, err)
	}

	webhookEvent, err := p.ParseWebhook(rawBody)
	if err != nil {
		event, storeErr := s.storeWebhookEvent(ctx, PaymentEventInput{
			Provider:       name,
			EventType:      "webhook.parse_failed",
			SignatureValid: true,
			PayloadHash:    payloadHash,
			DedupeKey:      dedupeKey(name, "parse", "", "", payloadHash),
			Headers:        headers,
			RawBody:        string(rawBody),
			Error:          err.Error(),
		})
		if storeErr != nil {
			s.log.Error("store unparsable payment webhook failed", "provider", name, "error", storeErr)
		}
		return WebhookResult{EventID: event.ID, Duplicate: event.Duplicate}, fmt.Errorf("%w: %v", ErrWebhookParseFailed, err)
	}

	event, err := s.storeWebhookEvent(ctx, PaymentEventInput{
		Provider:              name,
		EventType:             defaultString(webhookEvent.EventType, "payment.updated"),
		ProviderEventID:       webhookEvent.ProviderEventID,
		ProviderOrderID:       webhookEvent.ProviderOrderID,
		ProviderTransactionID: webhookEvent.ProviderTransactionID,
		SignatureValid:        true,
		PayloadHash:           payloadHash,
		DedupeKey:             dedupeKey(name, "valid", webhookEvent.ProviderEventID, webhookEvent.ProviderOrderID, payloadHash),
		Headers:               headers,
		RawBody:               string(rawBody),
		NormalizedStatus:      webhookEvent.NormalizedStatus,
	})
	if err != nil {
		return WebhookResult{}, fmt.Errorf("%w: %v", ErrPaymentEventPersistenceFailed, err)
	}
	if event.Duplicate {
		return WebhookResult{EventID: event.ID, Duplicate: true, Processed: true}, nil
	}

	if webhookEvent.EventType == "payout.updated" {
		return s.handlePayoutWebhook(ctx, name, event.ID, webhookEvent)
	}

	if strings.TrimSpace(webhookEvent.ProviderOrderID) == "" {
		if err := s.repo.MarkEventProcessed(ctx, event.ID, "", "provider order id missing"); err != nil {
			return WebhookResult{EventID: event.ID}, fmt.Errorf("mark payment event missing order id: %w", err)
		}
		return WebhookResult{EventID: event.ID, Processed: true}, nil
	}

	order, err := s.repo.GetPaymentOrderByProviderOrderID(ctx, name, webhookEvent.ProviderOrderID)
	if errors.Is(err, ErrPaymentOrderNotFound) {
		message := "payment order not found for provider order id"
		if markErr := s.repo.MarkEventProcessed(ctx, event.ID, "", message); markErr != nil {
			return WebhookResult{EventID: event.ID}, fmt.Errorf("mark unmatched payment event: %w", markErr)
		}
		s.log.Warn("payment webhook stored without matching order", "provider", name, "provider_order_id", webhookEvent.ProviderOrderID, "event_id", event.ID)
		return WebhookResult{EventID: event.ID, Processed: true, OrderFound: false}, nil
	}
	if err != nil {
		return WebhookResult{EventID: event.ID}, fmt.Errorf("find payment order for webhook: %w", err)
	}

	nextStatus := transitionStatus(order.Status, webhookEvent.NormalizedStatus)

	applyInput := ApplyWebhookEventInput{
		EventID:               event.ID,
		PaymentOrderID:        order.ID,
		FromStatus:            order.Status,
		ToStatus:              nextStatus,
		ProviderStatus:        webhookEvent.ProviderStatus,
		ProviderTransactionID: webhookEvent.ProviderTransactionID,
		Source:                "webhook",
	}
	paymentApp := s.applyLedgerFields(ctx, &applyInput, order, nextStatus)

	if _, err := s.repo.ApplyWebhookEvent(ctx, applyInput); err != nil {
		return WebhookResult{EventID: event.ID, OrderFound: true}, fmt.Errorf("apply payment webhook: %w", err)
	}
	if _, err := s.repo.CreateWebhookDeliveriesForEvent(ctx, event.ID, "payment.updated"); err != nil {
		s.log.Error("create payment webhook deliveries failed", "event_id", event.ID, "payment_order_id", order.ID, "error", err)
	} else {
		// Queued deliveries default to next_attempt_at = NOW(), so they're
		// immediately claimable — attempt delivery right now instead of
		// waiting for cmd/worker's next poll tick (up to
		// PAYMENTS_DELIVERY_POLL_INTERVAL later, or forever if that
		// separate worker process isn't deployed at all). This makes the
		// happy path independent of the worker; the worker remains the
		// backstop for retries and for any deployment where this inline
		// attempt itself fails. Runs detached from the request context so a
		// slow/unreachable app endpoint can't delay the response to the
		// payment provider.
		go s.deliverDueNow()
	}

	if applyInput.PostLedger {
		s.sendPaymentSuccessSMS(ctx, order, paymentApp.Name)
	}

	if s.notifier != nil && nextStatus == provider.StatusFailed && order.Status != provider.StatusFailed && s.notifyAllowed(ctx, "payment_failed") {
		_ = s.notifier.CreateAdminNotification(ctx, "payment_failed", "Payment failed",
			fmt.Sprintf("%s %s via %s", order.Amount, order.Currency, name),
			map[string]any{"payment_order_id": order.ID, "provider": name})
	}

	s.log.Info("payment webhook processed", "provider", name, "event_id", event.ID, "payment_order_id", order.ID, "from_status", order.Status, "to_status", nextStatus)
	return WebhookResult{EventID: event.ID, Processed: true, OrderFound: true}, nil
}

// applyLedgerFields decides whether an order-status transition should post
// ledger entries (the first genuine transition into "paid") or reverse them
// (paid -> reversed), filling the relevant fields on applyInput and
// returning the app record (needed by callers for the success SMS's app
// name). Shared by the webhook handler and applyProviderStatusUpdate
// (manual refresh / reconciliation) so ledger crediting behaves identically
// no matter how the "paid" transition was detected — previously only the
// webhook path posted ledger entries, so an order marked paid via
// refresh/reconciliation (e.g. because the provider's webhook never
// arrived) silently never credited the app's balance. Refund/reversal
// ledger handling for reconciliation/refresh-detected reversals is
// intentionally included too via the same PostRefund path already used by
// webhooks.
func (s *Service) applyLedgerFields(ctx context.Context, applyInput *ApplyWebhookEventInput, order PaymentOrder, nextStatus provider.Status) PaymentApp {
	var paymentApp PaymentApp
	if order.Status != provider.StatusPaid && nextStatus == provider.StatusPaid && order.AppID != "" {
		platformFee := "0"
		app, appErr := s.repo.GetPaymentAppByID(ctx, order.AppID)
		if appErr == nil {
			paymentApp = app
			platformFee = computePlatformFee(app.FeeType, app.FeePercent, app.FeeFixed, order.Amount)
		} else {
			s.log.Error("load payment app for fee computation failed", "app_id", order.AppID, "error", appErr)
		}
		applyInput.PostLedger = true
		applyInput.AppID = order.AppID
		applyInput.GrossAmount = order.Amount
		applyInput.Currency = order.Currency
		applyInput.PlatformFeeAmount = platformFee
	} else if order.Status == provider.StatusPaid && nextStatus == provider.StatusReversed && order.AppID != "" {
		// The provider reported this paid order as reversed/refunded (e.g.
		// SonicPesa's REVERSED/REFUNDED webhook statuses) — reverse the
		// original credit/fee in the same transaction as the status change.
		applyInput.PostRefund = true
		applyInput.AppID = order.AppID
	}
	return paymentApp
}

// sendPaymentSuccessSMS texts every app member with a phone number on file
// once an order genuinely settles into "paid" (called only when
// applyInput.PostLedger was true, so a replayed webhook never double-texts).
// Members who never added a phone in Account Settings are silently skipped
// — there is no other phone number anywhere in this system to fall back to
// (see PAYMENTS_GATEWAY_ARCHITECTURE.md); the merchant dashboard nudges them
// to add one instead. Best-effort throughout: an SMS failure must never
// fail webhook processing, since the payment itself already succeeded.
func (s *Service) sendPaymentSuccessSMS(ctx context.Context, order PaymentOrder, appName string) {
	if s.sms == nil {
		return
	}

	balance, err := s.repo.GetAppBalance(ctx, order.AppID)
	if err != nil {
		s.log.Error("load app balance for payment success sms failed", "app_id", order.AppID, "error", err)
		return
	}
	members, err := s.repo.ListAppMembers(ctx, order.AppID)
	if err != nil {
		s.log.Error("list app members for payment success sms failed", "app_id", order.AppID, "error", err)
		return
	}

	payer := order.BuyerName
	if payer == "" {
		payer = order.BuyerPhone
	}
	if payer == "" {
		payer = "a customer"
	}
	reference := order.ExternalReference
	if reference == "" {
		reference = order.ProviderOrderID
	}

	message := fmt.Sprintf(
		"AZSUBAY Payments: Confirmed. You've received %s %s from %s for %s. Ref: %s. New balance: %s %s.",
		order.Currency, order.Amount, payer, appName, reference, balance.Currency, balance.AvailableBalance,
	)

	for _, member := range members {
		if member.Phone == "" {
			continue
		}
		if err := s.sms.SendSMS(ctx, order.AppID, member.Phone, message); err != nil {
			s.log.Error("payment success sms failed", "app_id", order.AppID, "user_id", member.UserID, "error", err)
		}
	}
}

// handlePayoutWebhook applies a payout status callback to the matching
// withdrawal. ProviderOrderID carries the provider's payout ID for this
// branch (see sonicpesa.ParseWebhook). MarkWithdrawalPaid/MarkWithdrawalFailed
// are reused as-is — MarkWithdrawalFailed already reverses the ledger debit
// posted at approval time, so a payout that fails at the provider correctly
// restores the app's balance the same way a manual "mark failed" does.
func (s *Service) handlePayoutWebhook(ctx context.Context, providerName, eventID string, webhookEvent provider.WebhookEvent) (WebhookResult, error) {
	withdrawal, err := s.repo.GetWithdrawalByProviderPayoutID(ctx, providerName, webhookEvent.ProviderOrderID)
	if errors.Is(err, ErrWithdrawalNotFound) {
		message := "withdrawal not found for provider payout id"
		if markErr := s.repo.MarkEventProcessed(ctx, eventID, "", message); markErr != nil {
			return WebhookResult{EventID: eventID}, fmt.Errorf("mark unmatched payout event: %w", markErr)
		}
		s.log.Warn("payout webhook stored without matching withdrawal", "provider", providerName, "provider_payout_id", webhookEvent.ProviderOrderID, "event_id", eventID)
		return WebhookResult{EventID: eventID, Processed: true, OrderFound: false}, nil
	}
	if err != nil {
		return WebhookResult{EventID: eventID}, fmt.Errorf("find withdrawal for payout webhook: %w", err)
	}

	switch webhookEvent.NormalizedStatus {
	case provider.StatusPaid:
		if _, err := s.repo.MarkWithdrawalPaid(ctx, withdrawal.ID); err != nil && !errors.Is(err, ErrInvalidWithdrawalTransition) {
			return WebhookResult{EventID: eventID, OrderFound: true}, fmt.Errorf("apply payout paid webhook: %w", err)
		}
	case provider.StatusFailed, provider.StatusReversed:
		if _, err := s.repo.MarkWithdrawalFailed(ctx, withdrawal.ID, "Payout failed at provider: "+webhookEvent.ProviderStatus); err != nil && !errors.Is(err, ErrInvalidWithdrawalTransition) {
			return WebhookResult{EventID: eventID, OrderFound: true}, fmt.Errorf("apply payout failed webhook: %w", err)
		}
	default:
		// Still processing at the provider — nothing to apply until a
		// terminal status arrives (a later webhook, or reconciliation).
	}

	if err := s.repo.MarkEventProcessed(ctx, eventID, "", ""); err != nil {
		return WebhookResult{EventID: eventID}, fmt.Errorf("mark payout event processed: %w", err)
	}

	s.log.Info("payout webhook processed", "provider", providerName, "event_id", eventID, "withdrawal_id", withdrawal.ID, "status", webhookEvent.NormalizedStatus)
	return WebhookResult{EventID: eventID, Processed: true, OrderFound: true}, nil
}

func (s *Service) storeWebhookEvent(ctx context.Context, input PaymentEventInput) (StoredPaymentEvent, error) {
	if input.EventType == "" {
		input.EventType = "payment.updated"
	}
	event, err := s.repo.CreatePaymentEvent(ctx, input)
	if err != nil {
		return StoredPaymentEvent{}, err
	}
	return event, nil
}

func (s *Service) applyProviderStatusUpdate(ctx context.Context, order PaymentOrder, providerStatus provider.ProviderStatus, source string) (PaymentOrder, string, int64, error) {
	nextStatus := transitionStatus(order.Status, providerStatus.Status)
	eventID := ""
	var deliveriesCreated int64

	if nextStatus != order.Status {
		rawBody := providerStatusRawBody(providerStatus)
		event, err := s.storeWebhookEvent(ctx, PaymentEventInput{
			Provider:              order.Provider,
			EventType:             "payment.updated",
			ProviderOrderID:       defaultString(providerStatus.OrderID, order.ProviderOrderID),
			ProviderTransactionID: providerStatus.TransactionID,
			SignatureValid:        true,
			PayloadHash:           sha256Hex(rawBody),
			DedupeKey:             dedupeKey(order.Provider, source, "", order.ProviderOrderID, sha256Hex(rawBody)),
			Headers:               http.Header{"X-AZsubay-Source": []string{source}},
			RawBody:               string(rawBody),
			NormalizedStatus:      nextStatus,
		})
		if err != nil {
			return PaymentOrder{}, "", 0, fmt.Errorf("store %s payment event: %w", source, err)
		}
		eventID = event.ID
	}

	applyInput := ApplyWebhookEventInput{
		EventID:               eventID,
		PaymentOrderID:        order.ID,
		FromStatus:            order.Status,
		ToStatus:              nextStatus,
		ProviderStatus:        providerStatus.ProviderStatus,
		ProviderTransactionID: providerStatus.TransactionID,
		Source:                source,
	}
	paymentApp := s.applyLedgerFields(ctx, &applyInput, order, nextStatus)

	updated, err := s.repo.ApplyWebhookEvent(ctx, applyInput)
	if err != nil {
		return PaymentOrder{}, eventID, 0, err
	}

	if eventID != "" {
		deliveries, err := s.repo.CreateWebhookDeliveriesForEvent(ctx, eventID, "payment.updated")
		if err != nil {
			s.log.Error("create payment webhook deliveries failed", "event_id", eventID, "payment_order_id", updated.ID, "source", source, "error", err)
		} else {
			deliveriesCreated = deliveries
			if deliveries > 0 {
				go s.deliverDueNow()
			}
		}
	}

	// Same as the webhook path: notify the app's members the moment an
	// order genuinely settles into "paid", regardless of whether that was
	// detected live (webhook) or caught later (refresh/reconciliation).
	if applyInput.PostLedger {
		s.sendPaymentSuccessSMS(ctx, order, paymentApp.Name)
	}

	return updated, eventID, deliveriesCreated, nil
}

func transitionStatus(current, incoming provider.Status) provider.Status {
	if incoming == "" || incoming == provider.StatusUnknown {
		if current == "" {
			return provider.StatusUnknown
		}
		return current
	}
	if current == "" || current == provider.StatusUnknown {
		return incoming
	}
	if current == incoming {
		return current
	}
	if current == provider.StatusPaid && incoming == provider.StatusReversed {
		return provider.StatusReversed
	}
	if isFinalStatus(current) && isNonFinalStatus(incoming) {
		return current
	}
	if current == provider.StatusPaid && (incoming == provider.StatusFailed || incoming == provider.StatusCancelled || incoming == provider.StatusExpired) {
		return current
	}
	return incoming
}

func isFinalStatus(status provider.Status) bool {
	switch status {
	case provider.StatusPaid, provider.StatusFailed, provider.StatusCancelled, provider.StatusExpired, provider.StatusReversed:
		return true
	default:
		return false
	}
}

func isNonFinalStatus(status provider.Status) bool {
	return status == provider.StatusPending || status == provider.StatusProcessing || status == provider.StatusUnknown
}

func (s *Service) deliverWebhook(ctx context.Context, job PaymentWebhookDeliveryJob) (int, error) {
	payload := buildWebhookDeliveryPayload(job)
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payment webhook delivery payload: %w", err)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := signDeliveryPayload(s.endpointSigningSecret(job.EndpointID), timestamp, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build payment webhook delivery request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AZsubay-Event-ID", job.EventID)
	req.Header.Set("X-AZsubay-Timestamp", timestamp)
	req.Header.Set("X-AZsubay-Signature", signature)

	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func buildWebhookDeliveryPayload(job PaymentWebhookDeliveryJob) WebhookDeliveryPayload {
	eventType := defaultString(job.EventType, "payment.updated")
	occurredAt := job.ReceivedAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	data := map[string]any{}
	if job.RawProviderPayload != "" {
		var providerPayload map[string]any
		if err := json.Unmarshal([]byte(job.RawProviderPayload), &providerPayload); err == nil {
			data["provider_payload"] = providerPayload
		}
	}

	data["event_type"] = eventType

	return WebhookDeliveryPayload{
		ID:                job.EventID,
		Type:              "payment.updated",
		Provider:          job.Provider,
		PaymentID:         job.PaymentOrder.ID,
		ProviderOrderID:   job.PaymentOrder.ProviderOrderID,
		ExternalReference: job.PaymentOrder.ExternalReference,
		Status:            job.PaymentOrder.Status,
		Amount:            job.PaymentOrder.Amount,
		Currency:          job.PaymentOrder.Currency,
		Customer: map[string]string{
			"phone": job.PaymentOrder.BuyerPhone,
			"email": job.PaymentOrder.BuyerEmail,
			"name":  job.PaymentOrder.BuyerName,
		},
		OccurredAt: occurredAt,
		Data:       data,
	}
}

func (s *Service) endpointSigningSecret(endpointID string) string {
	mac := hmac.New(sha256.New, []byte(s.deliverySigningSecret))
	_, _ = mac.Write([]byte("azsubay-payment-webhook:"))
	_, _ = mac.Write([]byte(endpointID))
	return hex.EncodeToString(mac.Sum(nil))
}

func signDeliveryPayload(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func deliveryBackoff(attemptCount int) time.Duration {
	delay := 30 * time.Second
	for i := 1; i < attemptCount && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func deliveryErrorMessage(err error, status int) string {
	if err != nil {
		return err.Error()
	}
	if status > 0 {
		return fmt.Sprintf("endpoint returned status %d", status)
	}
	return "delivery failed"
}

func providerStatusRawBody(status provider.ProviderStatus) []byte {
	payload := status.Raw
	if payload == nil {
		payload = map[string]any{
			"order_id":          status.OrderID,
			"transaction_id":    status.TransactionID,
			"reference":         status.Reference,
			"amount":            status.Amount,
			"currency":          status.Currency,
			"phone":             status.Phone,
			"status":            status.ProviderStatus,
			"normalized_status": status.Status,
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func dedupeKey(providerName, kind, providerEventID, providerOrderID, payloadHash string) string {
	switch {
	case providerEventID != "":
		return providerName + ":event:" + providerEventID
	case providerOrderID != "":
		return providerName + ":order:" + providerOrderID + ":payload:" + payloadHash
	default:
		return providerName + ":" + kind + ":payload:" + payloadHash
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeEventTypes(eventTypes []string) []string {
	if len(eventTypes) == 0 {
		return []string{"payment.updated"}
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(eventTypes))
	for _, eventType := range eventTypes {
		eventType = strings.ToLower(strings.TrimSpace(eventType))
		if eventType == "" || seen[eventType] {
			continue
		}
		seen[eventType] = true
		out = append(out, eventType)
	}
	if len(out) == 0 {
		return []string{"payment.updated"}
	}
	return out
}

func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("invalid webhook scheme")
	}
	if parsed.Host == "" || parsed.User != nil {
		return errors.New("invalid webhook host")
	}
	return nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeCreateOrderInput(input CreatePaymentOrderInput) CreatePaymentOrderInput {
	input.Provider = normalizeProviderName(input.Provider)
	input.Amount = strings.TrimSpace(input.Amount)
	input.Currency = strings.ToUpper(strings.TrimSpace(defaultString(input.Currency, "TZS")))
	input.BuyerName = strings.TrimSpace(input.BuyerName)
	input.BuyerEmail = strings.TrimSpace(strings.ToLower(input.BuyerEmail))
	input.BuyerPhone = strings.TrimSpace(input.BuyerPhone)
	input.ExternalReference = strings.TrimSpace(input.ExternalReference)
	return input
}

func normalizeAmount(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("amount required")
	}

	amount, ok := new(big.Rat).SetString(value)
	if !ok || amount.Sign() <= 0 {
		return "", errors.New("invalid amount")
	}

	return amount.FloatString(2), nil
}

// computePlatformFee is deliberately clamped to [0, gross] and never errors
// — a missing/malformed fee config should degrade to "no fee charged"
// rather than block a payment from settling.
func computePlatformFee(feeType, feePercent, feeFixed, grossAmount string) string {
	gross, ok := new(big.Rat).SetString(grossAmount)
	if !ok {
		return "0.00"
	}
	percent, ok := new(big.Rat).SetString(feePercent)
	if !ok {
		percent = new(big.Rat)
	}
	fixed, ok := new(big.Rat).SetString(feeFixed)
	if !ok {
		fixed = new(big.Rat)
	}

	fee := new(big.Rat)
	switch feeType {
	case "fixed":
		fee.Set(fixed)
	case "hybrid":
		percentAmount := new(big.Rat).Mul(percent, gross)
		percentAmount.Quo(percentAmount, big.NewRat(100, 1))
		fee.Add(fixed, percentAmount)
	default: // "percentage"
		fee.Mul(percent, gross)
		fee.Quo(fee, big.NewRat(100, 1))
	}

	if fee.Sign() < 0 {
		fee = new(big.Rat)
	}
	if fee.Cmp(gross) > 0 {
		fee = gross
	}

	return fee.FloatString(2)
}

func copyMetadata(metadata map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomHex(size int) (string, error) {
	if size < 16 {
		size = 16
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate payment api key: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
