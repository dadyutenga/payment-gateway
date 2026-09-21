package payments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"

	"github.com/jackc/pgx"
)

var ErrPaymentOrderNotFound = errors.New("payment order not found")
var ErrPaymentAppNotFound = errors.New("payment app not found")
var ErrPaymentWebhookDeliveryNotFound = errors.New("payment webhook delivery not found")
var ErrPaymentProviderAccountNotFound = errors.New("payment provider account not found")

type Repository interface {
	GetAppByAPIKeyHash(ctx context.Context, keyHash string) (PaymentApp, error)
	GetPaymentAppByID(ctx context.Context, appID string) (PaymentApp, error)
	CreatePaymentApp(ctx context.Context, input CreatePaymentAppInput) (PaymentApp, error)
	CreatePaymentAPIKey(ctx context.Context, appID, keyHash string) error
	RevokePaymentAPIKeys(ctx context.Context, appID string) error
	ListPaymentApps(ctx context.Context, limit, offset int) (PaymentAppListResult, error)
	CreatePaymentOrder(ctx context.Context, input CreatePaymentOrderRepositoryInput) (PaymentOrder, error)
	GetPaymentOrderByIDForApp(ctx context.Context, appID, paymentOrderID string) (PaymentOrder, error)
	GetPaymentOrderByAppReference(ctx context.Context, appID, externalReference string) (PaymentOrder, error)
	CreatePaymentEvent(ctx context.Context, input PaymentEventInput) (StoredPaymentEvent, error)
	GetPaymentOrderByProviderOrderID(ctx context.Context, providerName, providerOrderID string) (PaymentOrder, error)
	ApplyWebhookEvent(ctx context.Context, input ApplyWebhookEventInput) (PaymentOrder, error)
	MarkEventProcessed(ctx context.Context, eventID, paymentOrderID, processingError string) error
	CreatePaymentWebhookEndpoint(ctx context.Context, input CreatePaymentWebhookEndpointRepositoryInput) (PaymentWebhookEndpoint, error)
	ListWebhookEndpoints(ctx context.Context, appID string) ([]PaymentWebhookEndpoint, error)
	CreateWebhookDeliveriesForEvent(ctx context.Context, eventID, eventType string) (int64, error)
	ReplayFailedWebhookDeliveriesForEvent(ctx context.Context, eventID string) (int64, error)
	ClaimDueWebhookDeliveries(ctx context.Context, limit int) ([]PaymentWebhookDeliveryJob, error)
	RecordWebhookDeliveryAttempt(ctx context.Context, input RecordWebhookDeliveryAttemptInput) error
	ListReconciliationCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]PaymentOrder, error)
	SearchPaymentOrders(ctx context.Context, filter PaymentOrderSearchFilter) (PaymentOrderSearchResult, error)
	ListPaymentEvents(ctx context.Context, filter PaymentEventListFilter) (PaymentEventListResult, error)
	ListWebhookDeliveries(ctx context.Context, filter PaymentWebhookDeliveryListFilter) (PaymentWebhookDeliveryListResult, error)
	ReplayWebhookDelivery(ctx context.Context, deliveryID string) error
	GetPaymentMetrics(ctx context.Context, staleBefore time.Time) (PaymentMetrics, error)
	CreateProviderAccount(ctx context.Context, provider, name, environment, baseURL, credentialsEncrypted string) (PaymentProviderAccount, error)
	UpdateProviderAccount(ctx context.Context, id, name, environment, baseURL, status string, credentialsEncrypted *string) (PaymentProviderAccount, error)
	DeleteProviderAccount(ctx context.Context, id string) error
	ListProviderAccounts(ctx context.Context) ([]PaymentProviderAccount, error)
	SetDefaultProviderAccount(ctx context.Context, id string) error
	GetDefaultProviderAccount(ctx context.Context, provider string) (PaymentProviderAccount, error)

	UpdateAppFees(ctx context.Context, appID string, input UpdateAppFeesInput) (PaymentApp, error)
	DeletePaymentApp(ctx context.Context, appID string) (PaymentApp, error)
	GetAppBalance(ctx context.Context, appID string) (AppBalance, error)
	ListLedgerEntries(ctx context.Context, filter LedgerEntryListFilter) (LedgerEntryListResult, error)
	CreateWithdrawal(ctx context.Context, input CreateWithdrawalInput) (PaymentWithdrawal, error)
	ListWithdrawals(ctx context.Context, filter WithdrawalListFilter) (WithdrawalListResult, error)
	GetWithdrawalByID(ctx context.Context, id string) (PaymentWithdrawal, error)
	ApproveWithdrawal(ctx context.Context, id, approvedBy string) (PaymentWithdrawal, error)
	RejectWithdrawal(ctx context.Context, id string) (PaymentWithdrawal, error)
	MarkWithdrawalPaid(ctx context.Context, id string) (PaymentWithdrawal, error)
	MarkWithdrawalFailed(ctx context.Context, id, notes string) (PaymentWithdrawal, error)
	DispatchWithdrawal(ctx context.Context, id, provider, providerPayoutID, status, providerStatus string) (PaymentWithdrawal, error)
	RecordPayoutFailure(ctx context.Context, id, provider, reason string) (PaymentWithdrawal, error)
	GetWithdrawalByProviderPayoutID(ctx context.Context, provider, providerPayoutID string) (PaymentWithdrawal, error)
	ListPayoutReconciliationCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]PaymentWithdrawal, error)

	FindUserIDByEmail(ctx context.Context, email string) (string, error)
	AddAppMember(ctx context.Context, appID, userID, addedBy string) (AppMember, error)
	ListAppMembers(ctx context.Context, appID string) ([]AppMember, error)
	RemoveAppMember(ctx context.Context, appID, userID string) error
	IsAppMember(ctx context.Context, userID, appID string) (bool, error)
	ListAppsForUser(ctx context.Context, userID string) ([]PaymentApp, error)

	GetPaymentOrderByID(ctx context.Context, paymentOrderID string) (PaymentOrder, error)
	RefundOrder(ctx context.Context, orderID, source string) (PaymentOrder, error)
}

type PostgresRepository struct {
	db *pgx.ConnPool
}

func NewPostgresRepository(db *pgx.ConnPool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) GetAppByAPIKeyHash(ctx context.Context, keyHash string) (PaymentApp, error) {
	const query = `
		SELECT a.id::text, a.name, COALESCE(a.description, ''), a.status,
		       a.fee_type, a.fee_percent::text, a.fee_fixed::text, a.created_at, a.updated_at
		FROM app.payment_api_keys k
		JOIN app.payment_apps a ON a.id = k.app_id
		WHERE k.key_hash = $1
		  AND k.revoked_at IS NULL
		  AND a.status = 'active'
		LIMIT 1
	`

	var app PaymentApp
	err := r.db.QueryRowEx(ctx, query, nil, keyHash).Scan(
		&app.ID,
		&app.Name,
		&app.Description,
		&app.Status,
		&app.FeeType,
		&app.FeePercent,
		&app.FeeFixed,
		&app.CreatedAt,
		&app.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentApp{}, ErrPaymentAppNotFound
	}
	if err != nil {
		return PaymentApp{}, fmt.Errorf("get payment app by api key: %w", err)
	}

	if _, err := r.db.ExecEx(ctx, `UPDATE app.payment_api_keys SET last_used_at = NOW() WHERE key_hash = $1`, nil, keyHash); err != nil {
		return PaymentApp{}, fmt.Errorf("update payment api key last used: %w", err)
	}

	return app, nil
}

func (r *PostgresRepository) CreatePaymentApp(ctx context.Context, input CreatePaymentAppInput) (PaymentApp, error) {
	const query = `
		INSERT INTO app.payment_apps (name, description)
		VALUES ($1, $2)
		RETURNING id::text, name, COALESCE(description, ''), status,
		          fee_type, fee_percent::text, fee_fixed::text, created_at, updated_at
	`

	var app PaymentApp
	err := r.db.QueryRowEx(ctx, query, nil, input.Name, valueOrNil(input.Description)).Scan(
		&app.ID,
		&app.Name,
		&app.Description,
		&app.Status,
		&app.FeeType,
		&app.FeePercent,
		&app.FeeFixed,
		&app.CreatedAt,
		&app.UpdatedAt,
	)
	if err != nil {
		return PaymentApp{}, fmt.Errorf("create payment app: %w", err)
	}
	return app, nil
}

func (r *PostgresRepository) GetPaymentAppByID(ctx context.Context, appID string) (PaymentApp, error) {
	const query = `
		SELECT id::text, name, COALESCE(description, ''), status,
		       fee_type, fee_percent::text, fee_fixed::text, created_at, updated_at
		FROM app.payment_apps
		WHERE id = $1::uuid
	`
	var app PaymentApp
	err := r.db.QueryRowEx(ctx, query, nil, appID).Scan(
		&app.ID, &app.Name, &app.Description, &app.Status, &app.FeeType, &app.FeePercent, &app.FeeFixed, &app.CreatedAt, &app.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentApp{}, ErrPaymentAppNotFound
	}
	if err != nil {
		return PaymentApp{}, fmt.Errorf("get payment app by id: %w", err)
	}
	return app, nil
}

func (r *PostgresRepository) CreatePaymentAPIKey(ctx context.Context, appID, keyHash string) error {
	const query = `
		INSERT INTO app.payment_api_keys (app_id, key_hash)
		VALUES ($1::uuid, $2)
	`
	if _, err := r.db.ExecEx(ctx, query, nil, appID, keyHash); err != nil {
		return fmt.Errorf("create payment api key: %w", err)
	}
	return nil
}

// RevokePaymentAPIKeys revokes every currently-active key for an app —
// called before issuing a new one, so an app only ever has at most one
// active key at a time.
func (r *PostgresRepository) RevokePaymentAPIKeys(ctx context.Context, appID string) error {
	const query = `
		UPDATE app.payment_api_keys
		SET revoked_at = NOW()
		WHERE app_id = $1::uuid AND revoked_at IS NULL
	`
	if _, err := r.db.ExecEx(ctx, query, nil, appID); err != nil {
		return fmt.Errorf("revoke payment api keys: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListPaymentApps(ctx context.Context, limit, offset int) (PaymentAppListResult, error) {
	limit, offset = boundedLimitOffset(limit, offset, 50, 200)

	var total int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.payment_apps WHERE status != 'deleted'`, nil).Scan(&total); err != nil {
		return PaymentAppListResult{}, fmt.Errorf("count payment apps: %w", err)
	}

	const query = `
		SELECT id::text, name, COALESCE(description, ''), status,
		       fee_type, fee_percent::text, fee_fixed::text, created_at, updated_at
		FROM app.payment_apps
		WHERE status != 'deleted'
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryEx(ctx, query, nil, limit, offset)
	if err != nil {
		return PaymentAppListResult{}, fmt.Errorf("list payment apps: %w", err)
	}
	defer rows.Close()

	apps := []PaymentApp{}
	for rows.Next() {
		var app PaymentApp
		if err := rows.Scan(&app.ID, &app.Name, &app.Description, &app.Status, &app.FeeType, &app.FeePercent, &app.FeeFixed, &app.CreatedAt, &app.UpdatedAt); err != nil {
			return PaymentAppListResult{}, fmt.Errorf("scan payment app: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return PaymentAppListResult{}, fmt.Errorf("iterate payment apps: %w", err)
	}

	return PaymentAppListResult{Items: apps, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *PostgresRepository) CreatePaymentOrder(ctx context.Context, input CreatePaymentOrderRepositoryInput) (PaymentOrder, error) {
	metadataJSON, err := marshalMetadata(input.Metadata)
	if err != nil {
		return PaymentOrder{}, err
	}

	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("begin create payment order transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	const query = `
		INSERT INTO app.payment_orders (
			app_id,
			provider,
			provider_order_id,
			provider_transaction_id,
			external_reference,
			amount,
			currency,
			buyer_name,
			buyer_email,
			buyer_phone,
			status,
			provider_status,
			metadata
		)
		VALUES ($1::uuid,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11,$12,$13::jsonb)
		RETURNING
			id::text,
			COALESCE(app_id::text, ''),
			provider,
			COALESCE(provider_order_id, ''),
			COALESCE(provider_transaction_id, ''),
			COALESCE(external_reference, ''),
			amount::text,
			currency,
			COALESCE(buyer_name, ''),
			COALESCE(buyer_email, ''),
			COALESCE(buyer_phone, ''),
			status,
			COALESCE(provider_status, ''),
			metadata::text,
			created_at,
			updated_at
	`

	order, err := scanPaymentOrder(tx.QueryRowEx(
		ctx,
		query,
		nil,
		input.AppID,
		input.Provider,
		valueOrNil(input.ProviderOrderID),
		valueOrNil(input.ProviderTransactionID),
		valueOrNil(input.ExternalReference),
		input.Amount,
		input.Currency,
		valueOrNil(input.BuyerName),
		valueOrNil(input.BuyerEmail),
		valueOrNil(input.BuyerPhone),
		string(input.Status),
		valueOrNil(input.ProviderStatus),
		string(metadataJSON),
	))
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("create payment order: %w", err)
	}

	const history = `
		INSERT INTO app.payment_status_history (
			payment_order_id,
			from_status,
			to_status,
			provider_status,
			source
		)
		VALUES ($1::uuid,NULL,$2,$3,'create_order')
	`
	if _, err = tx.ExecEx(ctx, history, nil, order.ID, string(order.Status), valueOrNil(order.ProviderStatus)); err != nil {
		return PaymentOrder{}, fmt.Errorf("insert payment order initial history: %w", err)
	}

	if err = tx.CommitEx(ctx); err != nil {
		return PaymentOrder{}, fmt.Errorf("commit create payment order transaction: %w", err)
	}

	return order, nil
}

// GetPaymentOrderByID is the admin-facing counterpart to
// GetPaymentOrderByIDForApp — no app_id filter, used by admin-only flows
// (e.g. RefundOrder) that aren't scoped to a single calling app.
func (r *PostgresRepository) GetPaymentOrderByID(ctx context.Context, paymentOrderID string) (PaymentOrder, error) {
	const query = paymentOrderSelect + `
		WHERE id = $1::uuid
		LIMIT 1
	`
	order, err := scanPaymentOrder(r.db.QueryRowEx(ctx, query, nil, paymentOrderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrPaymentOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("get payment order by id: %w", err)
	}
	return order, nil
}

func (r *PostgresRepository) GetPaymentOrderByIDForApp(ctx context.Context, appID, paymentOrderID string) (PaymentOrder, error) {
	const query = paymentOrderSelect + `
		WHERE id = $1::uuid AND app_id = $2::uuid
		LIMIT 1
	`
	order, err := scanPaymentOrder(r.db.QueryRowEx(ctx, query, nil, paymentOrderID, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrPaymentOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("get payment order by app: %w", err)
	}
	return order, nil
}

func (r *PostgresRepository) GetPaymentOrderByAppReference(ctx context.Context, appID, externalReference string) (PaymentOrder, error) {
	const query = paymentOrderSelect + `
		WHERE app_id = $1::uuid AND external_reference = $2
		LIMIT 1
	`
	order, err := scanPaymentOrder(r.db.QueryRowEx(ctx, query, nil, appID, externalReference))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrPaymentOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("get payment order by app reference: %w", err)
	}
	return order, nil
}

func (r *PostgresRepository) CreatePaymentEvent(ctx context.Context, input PaymentEventInput) (StoredPaymentEvent, error) {
	headersJSON, err := json.Marshal(input.Headers)
	if err != nil {
		return StoredPaymentEvent{}, fmt.Errorf("marshal payment event headers: %w", err)
	}

	const query = `
		INSERT INTO app.payment_events (
			provider,
			event_type,
			provider_event_id,
			provider_order_id,
			provider_transaction_id,
			signature_valid,
			payload_hash,
			dedupe_key,
			headers,
			raw_body,
			normalized_status,
			error
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
		ON CONFLICT (dedupe_key) DO UPDATE
		SET duplicate_count = app.payment_events.duplicate_count + 1,
		    last_seen_at = NOW()
		RETURNING id::text, duplicate_count > 0
	`

	var event StoredPaymentEvent
	err = r.db.QueryRowEx(
		ctx,
		query,
		nil,
		input.Provider,
		valueOrNil(input.EventType),
		valueOrNil(input.ProviderEventID),
		valueOrNil(input.ProviderOrderID),
		valueOrNil(input.ProviderTransactionID),
		input.SignatureValid,
		input.PayloadHash,
		input.DedupeKey,
		string(headersJSON),
		input.RawBody,
		valueOrNil(string(input.NormalizedStatus)),
		valueOrNil(input.Error),
	).Scan(&event.ID, &event.Duplicate)
	if err != nil {
		return StoredPaymentEvent{}, fmt.Errorf("insert payment event: %w", err)
	}

	return event, nil
}

func (r *PostgresRepository) GetPaymentOrderByProviderOrderID(ctx context.Context, providerName, providerOrderID string) (PaymentOrder, error) {
	const query = paymentOrderSelect + `
		WHERE provider = $1 AND provider_order_id = $2
		LIMIT 1
	`
	order, err := scanPaymentOrder(r.db.QueryRowEx(ctx, query, nil, providerName, providerOrderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrPaymentOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("get payment order by provider order id: %w", err)
	}

	return order, nil
}

// ApplyWebhookEvent updates a payment order's status and, in the same
// transaction, posts any ledger entries that transition implies. It backs
// all three ways an order's status can change — an incoming provider
// webhook, an app explicitly refreshing one order, and the background
// reconciliation sweep (input.Source records which) — so ledger crediting
// behaves identically no matter how the "paid" transition was detected.
// Previously only the webhook path posted ledger entries; an order marked
// paid via refresh/reconciliation (e.g. because the provider's webhook
// never arrived) silently never credited the app's balance, which is why
// apps could show 0.00 despite having real paid orders.
//
// Concurrency: the order row is locked with SELECT ... FOR UPDATE first and
// the decision to post ledger entries is re-derived from the freshly locked
// status — never from the caller's stale read. A webhook racing GetOrder's
// auto-refresh, or racing reconciliation (including across cmd/api +
// cmd/worker processes), serializes on the row lock; the loser sees the
// already-applied status and skips the ledger insert. Partial unique indexes
// on (payment_order_id) WHERE entry_type IN (...) are the second line of
// defence: even if two transactions ever both attempt the insert, the second
// gets ON CONFLICT DO NOTHING instead of a double credit.
func (r *PostgresRepository) ApplyWebhookEvent(ctx context.Context, input ApplyWebhookEventInput) (PaymentOrder, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("begin payment webhook transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	// Lock the order first so concurrent webhook / refresh / reconciliation
	// workers serialize here instead of both reading a stale status.
	locked, err := scanPaymentOrder(tx.QueryRowEx(ctx, paymentOrderSelect+` WHERE id = $1::uuid FOR UPDATE`, nil, input.PaymentOrderID))
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("lock payment order: %w", err)
	}

	// Re-check against the freshly locked status. input.FromStatus /
	// input.PostLedger were computed from a read that may now be stale.
	actualFrom := locked.Status
	shouldPostLedger := input.PostLedger &&
		actualFrom != provider.StatusPaid &&
		input.ToStatus == provider.StatusPaid
	shouldPostRefund := input.PostRefund &&
		actualFrom == provider.StatusPaid &&
		input.ToStatus == provider.StatusReversed
	statusChanged := actualFrom != input.ToStatus

	const updateOrder = `
		UPDATE app.payment_orders
		SET status = $2,
		    provider_status = $3,
		    provider_transaction_id = COALESCE(NULLIF($4, ''), provider_transaction_id),
		    updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			COALESCE(app_id::text, ''),
			provider,
			COALESCE(provider_order_id, ''),
			COALESCE(provider_transaction_id, ''),
			COALESCE(external_reference, ''),
			amount::text,
			currency,
			COALESCE(buyer_name, ''),
			COALESCE(buyer_email, ''),
			COALESCE(buyer_phone, ''),
			status,
			COALESCE(provider_status, ''),
			metadata::text,
			created_at,
			updated_at
	`
	order, err := scanPaymentOrder(tx.QueryRowEx(ctx, updateOrder, nil, input.PaymentOrderID, string(input.ToStatus), valueOrNil(input.ProviderStatus), input.ProviderTransactionID))
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("update payment order from webhook: %w", err)
	}

	if statusChanged {
		const insertHistory = `
			INSERT INTO app.payment_status_history (
				payment_order_id,
				from_status,
				to_status,
				provider_status,
				source,
				event_id
			)
			VALUES ($1::uuid,$2,$3,$4,$5,$6::uuid)
		`
		if _, err = tx.ExecEx(ctx, insertHistory, nil, input.PaymentOrderID, valueOrNil(string(actualFrom)), string(input.ToStatus), valueOrNil(input.ProviderStatus), input.Source, valueOrNil(input.EventID)); err != nil {
			return PaymentOrder{}, fmt.Errorf("insert payment status history: %w", err)
		}
	}

	if input.EventID != "" {
		const updateEvent = `
			UPDATE app.payment_events
			SET payment_order_id = $2::uuid,
			    processed_at = NOW(),
			    error = NULL
			WHERE id = $1::uuid
		`
		if _, err = tx.ExecEx(ctx, updateEvent, nil, input.EventID, input.PaymentOrderID); err != nil {
			return PaymentOrder{}, fmt.Errorf("mark payment event processed: %w", err)
		}
	}

	// Post the settlement ledger entries in the same transaction as the
	// status change, so an order can never be marked "paid" without the
	// app's balance updating, or vice versa. Only fires once per order —
	// shouldPostLedger is re-derived from the locked row above, so a
	// duplicate/replayed webhook, or a refresh/reconciliation re-confirming
	// an already-paid order, never double-credits. The partial unique
	// indexes from migration 000029 make the inserts idempotent even if two
	// transactions ever both reach this point: the loser hits ON CONFLICT
	// DO NOTHING instead of posting a second credit.
	if shouldPostLedger {
		const insertCredit = `
			INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, description)
			VALUES ($1::uuid, $2::uuid, 'payment_credit', 'credit', $3::numeric, $4, $5)
			ON CONFLICT DO NOTHING
		`
		if _, err = tx.ExecEx(ctx, insertCredit, nil, input.AppID, input.PaymentOrderID, input.GrossAmount, input.Currency, "Payment received"); err != nil {
			return PaymentOrder{}, fmt.Errorf("insert payment credit ledger entry: %w", err)
		}

		if input.PlatformFeeAmount != "" && input.PlatformFeeAmount != "0" {
			const insertFee = `
				INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, description)
				VALUES ($1::uuid, $2::uuid, 'platform_fee_debit', 'debit', $3::numeric, $4, $5)
				ON CONFLICT DO NOTHING
			`
			if _, err = tx.ExecEx(ctx, insertFee, nil, input.AppID, input.PaymentOrderID, input.PlatformFeeAmount, input.Currency, "Platform fee"); err != nil {
				return PaymentOrder{}, fmt.Errorf("insert platform fee ledger entry: %w", err)
			}
		}
	}

	if shouldPostRefund {
		if err = postRefundEntries(ctx, tx, input.AppID, input.PaymentOrderID); err != nil {
			return PaymentOrder{}, fmt.Errorf("post refund ledger entries: %w", err)
		}
	}

	if err = tx.CommitEx(ctx); err != nil {
		return PaymentOrder{}, fmt.Errorf("commit payment webhook transaction: %w", err)
	}
	return order, nil
}

func (r *PostgresRepository) MarkEventProcessed(ctx context.Context, eventID, paymentOrderID, processingError string) error {
	if paymentOrderID != "" {
		const query = `
			UPDATE app.payment_events
			SET payment_order_id = $2::uuid,
			    processed_at = NOW(),
			    error = $3
			WHERE id = $1::uuid
		`
		if _, err := r.db.ExecEx(ctx, query, nil, eventID, paymentOrderID, valueOrNil(processingError)); err != nil {
			return fmt.Errorf("mark payment event processed: %w", err)
		}
		return nil
	}

	const query = `
		UPDATE app.payment_events
		SET processed_at = NOW(),
		    error = $2
		WHERE id = $1::uuid
	`
	if _, err := r.db.ExecEx(ctx, query, nil, eventID, valueOrNil(processingError)); err != nil {
		return fmt.Errorf("mark payment event processed: %w", err)
	}
	return nil
}

func (r *PostgresRepository) CreatePaymentWebhookEndpoint(ctx context.Context, input CreatePaymentWebhookEndpointRepositoryInput) (PaymentWebhookEndpoint, error) {
	const query = `
		INSERT INTO app.payment_webhook_endpoints (id, app_id, url, event_types, secret_hash)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5)
		RETURNING id::text, app_id::text, url, event_types, status, created_at, updated_at
	`

	var endpoint PaymentWebhookEndpoint
	err := r.db.QueryRowEx(ctx, query, nil, input.ID, input.AppID, input.URL, input.EventTypes, input.SecretHash).Scan(
		&endpoint.ID,
		&endpoint.AppID,
		&endpoint.URL,
		&endpoint.EventTypes,
		&endpoint.Status,
		&endpoint.CreatedAt,
		&endpoint.UpdatedAt,
	)
	if err != nil {
		return PaymentWebhookEndpoint{}, fmt.Errorf("create payment webhook endpoint: %w", err)
	}
	return endpoint, nil
}

func (r *PostgresRepository) ListWebhookEndpoints(ctx context.Context, appID string) ([]PaymentWebhookEndpoint, error) {
	conditions := []string{"1=1"}
	args := []interface{}{}
	if appID != "" {
		conditions = append(conditions, "app_id = $1::uuid")
		args = append(args, appID)
	}

	query := fmt.Sprintf(`
		SELECT id::text, app_id::text, url, event_types, status, created_at, updated_at
		FROM app.payment_webhook_endpoints
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(conditions, " AND "))

	rows, err := r.db.QueryEx(ctx, query, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("list payment webhook endpoints: %w", err)
	}
	defer rows.Close()

	endpoints := []PaymentWebhookEndpoint{}
	for rows.Next() {
		var endpoint PaymentWebhookEndpoint
		if err := rows.Scan(
			&endpoint.ID, &endpoint.AppID, &endpoint.URL, &endpoint.EventTypes,
			&endpoint.Status, &endpoint.CreatedAt, &endpoint.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan payment webhook endpoint: %w", err)
		}
		endpoints = append(endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment webhook endpoints: %w", err)
	}
	return endpoints, nil
}

func (r *PostgresRepository) CreateWebhookDeliveriesForEvent(ctx context.Context, eventID, eventType string) (int64, error) {
	const query = `
		INSERT INTO app.payment_webhook_deliveries (event_id, endpoint_id)
		SELECT e.id, ep.id
		FROM app.payment_events e
		JOIN app.payment_orders o ON o.id = e.payment_order_id
		JOIN app.payment_webhook_endpoints ep ON ep.app_id = o.app_id
		WHERE e.id = $1::uuid
		  AND ep.status = 'active'
		  AND $2 = ANY(ep.event_types)
		ON CONFLICT (event_id, endpoint_id) DO NOTHING
	`
	tag, err := r.db.ExecEx(ctx, query, nil, eventID, eventType)
	if err != nil {
		return 0, fmt.Errorf("create payment webhook deliveries: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PostgresRepository) ReplayFailedWebhookDeliveriesForEvent(ctx context.Context, eventID string) (int64, error) {
	const query = `
		UPDATE app.payment_webhook_deliveries
		SET status = 'pending',
		    next_attempt_at = NOW(),
		    last_error = NULL,
		    last_response_status = NULL
		WHERE event_id = $1::uuid
		  AND status = 'failed'
	`
	tag, err := r.db.ExecEx(ctx, query, nil, eventID)
	if err != nil {
		return 0, fmt.Errorf("replay failed payment webhook deliveries for event: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ClaimDueWebhookDeliveries atomically claims due deliveries and reclaims
// stale 'processing' rows whose lease expired (crashed worker, shutdown, or
// cancelled context between claim and result-recording). Claimed rows get a
// fresh 5-minute lease; RecordWebhookDeliveryAttempt clears it on completion.
func (r *PostgresRepository) ClaimDueWebhookDeliveries(ctx context.Context, limit int) ([]PaymentWebhookDeliveryJob, error) {	if limit <= 0 {
		limit = 25
	}

	const query = `
		WITH due AS (
			SELECT id
			FROM app.payment_webhook_deliveries
			WHERE (
				(status IN ('pending', 'retrying') AND next_attempt_at <= NOW())
				OR (status = 'processing' AND lease_expires_at IS NOT NULL AND lease_expires_at <= NOW())
			)
			ORDER BY next_attempt_at ASC, created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		),
		claimed AS (
			UPDATE app.payment_webhook_deliveries d
			SET status = 'processing',
			    attempt_count = d.attempt_count + 1,
			    last_attempt_at = NOW(),
			    claimed_at = NOW(),
			    lease_expires_at = NOW() + INTERVAL '5 minutes'
			FROM due
			WHERE d.id = due.id
			RETURNING d.id, d.event_id, d.endpoint_id, d.attempt_count
		)
		SELECT
			c.id::text,
			c.event_id::text,
			c.endpoint_id::text,
			c.attempt_count,
			ep.url,
			ep.app_id::text,
			COALESCE(e.event_type, 'payment.updated'),
			e.provider,
			COALESCE(e.provider_order_id, ''),
			COALESCE(e.provider_transaction_id, ''),
			e.received_at,
			e.raw_body,
			o.id::text,
			COALESCE(o.app_id::text, ''),
			o.provider,
			COALESCE(o.provider_order_id, ''),
			COALESCE(o.provider_transaction_id, ''),
			COALESCE(o.external_reference, ''),
			o.amount::text,
			o.currency,
			COALESCE(o.buyer_name, ''),
			COALESCE(o.buyer_email, ''),
			COALESCE(o.buyer_phone, ''),
			o.status,
			COALESCE(o.provider_status, ''),
			o.metadata::text,
			o.created_at,
			o.updated_at
		FROM claimed c
		JOIN app.payment_webhook_endpoints ep ON ep.id = c.endpoint_id
		JOIN app.payment_events e ON e.id = c.event_id
		JOIN app.payment_orders o ON o.id = e.payment_order_id
		ORDER BY c.attempt_count ASC
	`

	rows, err := r.db.QueryEx(ctx, query, nil, limit)
	if err != nil {
		return nil, fmt.Errorf("claim payment webhook deliveries: %w", err)
	}
	defer rows.Close()

	jobs := []PaymentWebhookDeliveryJob{}
	for rows.Next() {
		job, err := scanPaymentWebhookDeliveryJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment webhook delivery job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment webhook deliveries: %w", err)
	}
	return jobs, nil
}

func (r *PostgresRepository) RecordWebhookDeliveryAttempt(ctx context.Context, input RecordWebhookDeliveryAttemptInput) error {
	status := "retrying"
	var deliveredAt any
	var nextAttemptAt any
	if input.Success {
		status = "delivered"
		deliveredAt = time.Now().UTC()
	}
	if input.Failed {
		status = "failed"
	}
	if input.NextAttemptAt != nil {
		nextAttemptAt = *input.NextAttemptAt
	}

	const query = `
		UPDATE app.payment_webhook_deliveries
		SET status = $2,
		    next_attempt_at = COALESCE($3, next_attempt_at),
		    last_response_status = $4,
		    last_error = $5,
		    delivered_at = COALESCE($6, delivered_at),
		    lease_expires_at = NULL
		WHERE id = $1::uuid
	`
	if _, err := r.db.ExecEx(ctx, query, nil, input.DeliveryID, status, nextAttemptAt, input.ResponseStatus, valueOrNil(input.Error), deliveredAt); err != nil {
		return fmt.Errorf("record payment webhook delivery attempt: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListReconciliationCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]PaymentOrder, error) {
	if limit <= 0 {
		limit = 50
	}

	const query = paymentOrderSelect + `
		WHERE provider_order_id IS NOT NULL
		  AND provider_order_id <> ''
		  AND status IN ('pending', 'processing', 'unknown')
		  AND updated_at <= $1
		ORDER BY updated_at ASC, created_at ASC
		LIMIT $2
	`

	rows, err := r.db.QueryEx(ctx, query, nil, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list payment reconciliation candidates: %w", err)
	}
	defer rows.Close()

	orders := []PaymentOrder{}
	for rows.Next() {
		order, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment reconciliation candidate: %w", err)
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment reconciliation candidates: %w", err)
	}
	return orders, nil
}

func (r *PostgresRepository) SearchPaymentOrders(ctx context.Context, filter PaymentOrderSearchFilter) (PaymentOrderSearchResult, error) {
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)

	conditions := []string{"1=1"}
	args := []any{}
	addCondition := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	if filter.Query != "" {
		args = append(args, filter.Query)
		arg := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			id::text ILIKE '%%' || $%d || '%%'
			OR provider_order_id ILIKE '%%' || $%d || '%%'
			OR provider_transaction_id ILIKE '%%' || $%d || '%%'
			OR external_reference ILIKE '%%' || $%d || '%%'
			OR buyer_phone ILIKE '%%' || $%d || '%%'
			OR buyer_email ILIKE '%%' || $%d || '%%'
		)`, arg, arg, arg, arg, arg, arg))
	}
	if filter.AppID != "" {
		addCondition("app_id = $%d::uuid", filter.AppID)
	}
	if filter.Provider != "" {
		addCondition("provider = $%d", filter.Provider)
	}
	if filter.Status != "" {
		addCondition("status = $%d", string(filter.Status))
	}
	if filter.Phone != "" {
		addCondition("buyer_phone ILIKE '%%' || $%d || '%%'", filter.Phone)
	}
	if filter.PaymentID != "" {
		addCondition("id = $%d::uuid", filter.PaymentID)
	}
	if filter.ProviderOrderID != "" {
		addCondition("provider_order_id = $%d", filter.ProviderOrderID)
	}
	if filter.ExternalReference != "" {
		addCondition("external_reference = $%d", filter.ExternalReference)
	}

	where := strings.Join(conditions, " AND ")
	countQuery := "SELECT COUNT(*) FROM app.payment_orders WHERE " + where
	var total int64
	if err := r.db.QueryRowEx(ctx, countQuery, nil, args...).Scan(&total); err != nil {
		return PaymentOrderSearchResult{}, fmt.Errorf("count payment orders: %w", err)
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	limitArg := len(listArgs) - 1
	offsetArg := len(listArgs)
	query := paymentOrderSelect + `
		WHERE ` + where + fmt.Sprintf(`
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, limitArg, offsetArg)

	rows, err := r.db.QueryEx(ctx, query, nil, listArgs...)
	if err != nil {
		return PaymentOrderSearchResult{}, fmt.Errorf("search payment orders: %w", err)
	}
	defer rows.Close()

	orders := []PaymentOrder{}
	for rows.Next() {
		order, err := scanPaymentOrder(rows)
		if err != nil {
			return PaymentOrderSearchResult{}, fmt.Errorf("scan payment order search result: %w", err)
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return PaymentOrderSearchResult{}, fmt.Errorf("iterate payment order search results: %w", err)
	}

	return PaymentOrderSearchResult{Items: orders, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *PostgresRepository) ListPaymentEvents(ctx context.Context, filter PaymentEventListFilter) (PaymentEventListResult, error) {
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)

	conditions := []string{"1=1"}
	args := []any{}
	addCondition := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	if filter.Provider != "" {
		addCondition("e.provider = $%d", filter.Provider)
	}
	if filter.EventType != "" {
		addCondition("e.event_type = $%d", filter.EventType)
	}
	if filter.PaymentOrderID != "" {
		addCondition("e.payment_order_id = $%d::uuid", filter.PaymentOrderID)
	}
	if filter.ProviderOrderID != "" {
		addCondition("e.provider_order_id = $%d", filter.ProviderOrderID)
	}
	if filter.Status != "" {
		addCondition("e.normalized_status = $%d", string(filter.Status))
	}
	if filter.Processed != nil {
		if *filter.Processed {
			conditions = append(conditions, "e.processed_at IS NOT NULL")
		} else {
			conditions = append(conditions, "e.processed_at IS NULL")
		}
	}
	if filter.HasError != nil {
		if *filter.HasError {
			conditions = append(conditions, "e.error IS NOT NULL AND e.error <> ''")
		} else {
			conditions = append(conditions, "(e.error IS NULL OR e.error = '')")
		}
	}
	if filter.SignatureValid != nil {
		addCondition("e.signature_valid = $%d", *filter.SignatureValid)
	}

	from := `
		FROM app.payment_events e
		LEFT JOIN app.payment_orders o ON o.id = e.payment_order_id
	`
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowEx(ctx, "SELECT COUNT(*) "+from+" WHERE "+where, nil, args...).Scan(&total); err != nil {
		return PaymentEventListResult{}, fmt.Errorf("count payment events: %w", err)
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	limitArg := len(listArgs) - 1
	offsetArg := len(listArgs)
	query := paymentEventSelect + from + " WHERE " + where + fmt.Sprintf(`
		ORDER BY e.received_at DESC
		LIMIT $%d OFFSET $%d
	`, limitArg, offsetArg)

	rows, err := r.db.QueryEx(ctx, query, nil, listArgs...)
	if err != nil {
		return PaymentEventListResult{}, fmt.Errorf("list payment events: %w", err)
	}
	defer rows.Close()

	events := []PaymentEvent{}
	for rows.Next() {
		event, err := scanPaymentEvent(rows)
		if err != nil {
			return PaymentEventListResult{}, fmt.Errorf("scan payment event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return PaymentEventListResult{}, fmt.Errorf("iterate payment events: %w", err)
	}

	return PaymentEventListResult{Items: events, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *PostgresRepository) ListWebhookDeliveries(ctx context.Context, filter PaymentWebhookDeliveryListFilter) (PaymentWebhookDeliveryListResult, error) {
	filter.Limit, filter.Offset = boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)

	conditions := []string{"1=1"}
	args := []any{}
	addCondition := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	if filter.Status != "" {
		addCondition("d.status = $%d", filter.Status)
	}
	if filter.EventID != "" {
		addCondition("d.event_id = $%d::uuid", filter.EventID)
	}
	if filter.EndpointID != "" {
		addCondition("d.endpoint_id = $%d::uuid", filter.EndpointID)
	}
	if filter.PaymentOrderID != "" {
		addCondition("o.id = $%d::uuid", filter.PaymentOrderID)
	}

	from := `
		FROM app.payment_webhook_deliveries d
		JOIN app.payment_webhook_endpoints ep ON ep.id = d.endpoint_id
		JOIN app.payment_events e ON e.id = d.event_id
		JOIN app.payment_orders o ON o.id = e.payment_order_id
	`
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowEx(ctx, "SELECT COUNT(*) "+from+" WHERE "+where, nil, args...).Scan(&total); err != nil {
		return PaymentWebhookDeliveryListResult{}, fmt.Errorf("count payment webhook deliveries: %w", err)
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	limitArg := len(listArgs) - 1
	offsetArg := len(listArgs)
	query := paymentWebhookDeliverySelect + from + " WHERE " + where + fmt.Sprintf(`
		ORDER BY d.created_at DESC
		LIMIT $%d OFFSET $%d
	`, limitArg, offsetArg)

	rows, err := r.db.QueryEx(ctx, query, nil, listArgs...)
	if err != nil {
		return PaymentWebhookDeliveryListResult{}, fmt.Errorf("list payment webhook deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := []PaymentWebhookDelivery{}
	for rows.Next() {
		delivery, err := scanPaymentWebhookDelivery(rows)
		if err != nil {
			return PaymentWebhookDeliveryListResult{}, fmt.Errorf("scan payment webhook delivery: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return PaymentWebhookDeliveryListResult{}, fmt.Errorf("iterate payment webhook deliveries: %w", err)
	}

	return PaymentWebhookDeliveryListResult{Items: deliveries, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *PostgresRepository) ReplayWebhookDelivery(ctx context.Context, deliveryID string) error {
	const query = `
		UPDATE app.payment_webhook_deliveries
		SET status = 'pending',
		    next_attempt_at = NOW(),
		    last_error = NULL,
		    last_response_status = NULL
		WHERE id = $1::uuid
		  AND status = 'failed'
	`
	tag, err := r.db.ExecEx(ctx, query, nil, deliveryID)
	if err != nil {
		return fmt.Errorf("replay payment webhook delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentWebhookDeliveryNotFound
	}
	return nil
}

func (r *PostgresRepository) GetPaymentMetrics(ctx context.Context, staleBefore time.Time) (PaymentMetrics, error) {
	metrics := PaymentMetrics{
		GeneratedAt:        time.Now().UTC(),
		OrdersByStatus:     map[string]int64{},
		DeliveriesByStatus: map[string]int64{},
	}

	orderRows, err := r.db.QueryEx(ctx, `
		SELECT status, COUNT(*)
		FROM app.payment_orders
		GROUP BY status
	`, nil)
	if err != nil {
		return PaymentMetrics{}, fmt.Errorf("load payment order metrics: %w", err)
	}
	for orderRows.Next() {
		var status string
		var count int64
		if err := orderRows.Scan(&status, &count); err != nil {
			orderRows.Close()
			return PaymentMetrics{}, fmt.Errorf("scan payment order metrics: %w", err)
		}
		metrics.OrdersByStatus[status] = count
	}
	if err := orderRows.Err(); err != nil {
		orderRows.Close()
		return PaymentMetrics{}, fmt.Errorf("iterate payment order metrics: %w", err)
	}
	orderRows.Close()

	deliveryRows, err := r.db.QueryEx(ctx, `
		SELECT status, COUNT(*)
		FROM app.payment_webhook_deliveries
		GROUP BY status
	`, nil)
	if err != nil {
		return PaymentMetrics{}, fmt.Errorf("load payment delivery metrics: %w", err)
	}
	for deliveryRows.Next() {
		var status string
		var count int64
		if err := deliveryRows.Scan(&status, &count); err != nil {
			deliveryRows.Close()
			return PaymentMetrics{}, fmt.Errorf("scan payment delivery metrics: %w", err)
		}
		metrics.DeliveriesByStatus[status] = count
	}
	if err := deliveryRows.Err(); err != nil {
		deliveryRows.Close()
		return PaymentMetrics{}, fmt.Errorf("iterate payment delivery metrics: %w", err)
	}
	deliveryRows.Close()

	const eventMetrics = `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE signature_valid = false),
			COALESCE(SUM(duplicate_count), 0),
			COUNT(*) FILTER (WHERE processed_at IS NULL),
			COUNT(*) FILTER (WHERE error IS NOT NULL AND error <> '')
		FROM app.payment_events
	`
	if err := r.db.QueryRowEx(ctx, eventMetrics, nil).Scan(
		&metrics.TotalEvents,
		&metrics.InvalidSignatureEvents,
		&metrics.DuplicateCallbacks,
		&metrics.UnprocessedEvents,
		&metrics.EventErrors,
	); err != nil {
		return PaymentMetrics{}, fmt.Errorf("load payment event metrics: %w", err)
	}

	const reconciliationCandidates = `
		SELECT COUNT(*)
		FROM app.payment_orders
		WHERE provider_order_id IS NOT NULL
		  AND provider_order_id <> ''
		  AND status IN ('pending', 'processing', 'unknown')
		  AND updated_at <= $1
	`
	if err := r.db.QueryRowEx(ctx, reconciliationCandidates, nil, staleBefore).Scan(&metrics.ReconciliationCandidates); err != nil {
		return PaymentMetrics{}, fmt.Errorf("load payment reconciliation metrics: %w", err)
	}

	return metrics, nil
}

func valueOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const paymentOrderSelect = `
	SELECT
		id::text,
		COALESCE(app_id::text, ''),
		provider,
		COALESCE(provider_order_id, ''),
		COALESCE(provider_transaction_id, ''),
		COALESCE(external_reference, ''),
		amount::text,
		currency,
		COALESCE(buyer_name, ''),
		COALESCE(buyer_email, ''),
		COALESCE(buyer_phone, ''),
		status,
		COALESCE(provider_status, ''),
		metadata::text,
		created_at,
		updated_at
	FROM app.payment_orders
`

const paymentEventSelect = `
	SELECT
		e.id::text,
		e.provider,
		e.event_type,
		COALESCE(e.provider_event_id, ''),
		COALESCE(e.provider_order_id, ''),
		COALESCE(e.provider_transaction_id, ''),
		COALESCE(e.payment_order_id::text, ''),
		COALESCE(o.app_id::text, ''),
		COALESCE(o.external_reference, ''),
		e.signature_valid,
		e.payload_hash,
		COALESCE(e.normalized_status, ''),
		e.received_at,
		e.processed_at,
		e.last_seen_at,
		e.duplicate_count,
		COALESCE(e.error, '')
`

const paymentWebhookDeliverySelect = `
	SELECT
		d.id::text,
		d.event_id::text,
		d.endpoint_id::text,
		ep.url,
		ep.app_id::text,
		d.status,
		d.attempt_count,
		d.next_attempt_at,
		d.last_attempt_at,
		d.last_response_status,
		COALESCE(d.last_error, ''),
		d.delivered_at,
		d.created_at,
		COALESCE(e.event_type, 'payment.updated'),
		e.provider,
		COALESCE(e.provider_order_id, ''),
		COALESCE(e.provider_transaction_id, ''),
		o.id::text,
		COALESCE(o.app_id::text, ''),
		o.provider,
		COALESCE(o.provider_order_id, ''),
		COALESCE(o.provider_transaction_id, ''),
		COALESCE(o.external_reference, ''),
		o.amount::text,
		o.currency,
		COALESCE(o.buyer_name, ''),
		COALESCE(o.buyer_email, ''),
		COALESCE(o.buyer_phone, ''),
		o.status,
		COALESCE(o.provider_status, ''),
		o.metadata::text,
		o.created_at,
		o.updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPaymentOrder(row rowScanner) (PaymentOrder, error) {
	var order PaymentOrder
	var status string
	var metadata string
	if err := row.Scan(
		&order.ID,
		&order.AppID,
		&order.Provider,
		&order.ProviderOrderID,
		&order.ProviderTransactionID,
		&order.ExternalReference,
		&order.Amount,
		&order.Currency,
		&order.BuyerName,
		&order.BuyerEmail,
		&order.BuyerPhone,
		&status,
		&order.ProviderStatus,
		&metadata,
		&order.CreatedAt,
		&order.UpdatedAt,
	); err != nil {
		return PaymentOrder{}, err
	}

	order.Status = provider.Status(status)
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &order.Metadata); err != nil {
			return PaymentOrder{}, fmt.Errorf("decode payment order metadata: %w", err)
		}
	}
	if order.Metadata == nil {
		order.Metadata = map[string]any{}
	}

	return order, nil
}

func scanPaymentEvent(row rowScanner) (PaymentEvent, error) {
	var event PaymentEvent
	var normalizedStatus string
	var processedAt sql.NullTime
	if err := row.Scan(
		&event.ID,
		&event.Provider,
		&event.EventType,
		&event.ProviderEventID,
		&event.ProviderOrderID,
		&event.ProviderTransactionID,
		&event.PaymentOrderID,
		&event.AppID,
		&event.ExternalReference,
		&event.SignatureValid,
		&event.PayloadHash,
		&normalizedStatus,
		&event.ReceivedAt,
		&processedAt,
		&event.LastSeenAt,
		&event.DuplicateCount,
		&event.Error,
	); err != nil {
		return PaymentEvent{}, err
	}

	event.NormalizedStatus = provider.Status(normalizedStatus)
	if processedAt.Valid {
		event.ProcessedAt = &processedAt.Time
	}
	return event, nil
}

func marshalMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	out, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal payment order metadata: %w", err)
	}
	return out, nil
}

func scanPaymentWebhookDelivery(row rowScanner) (PaymentWebhookDelivery, error) {
	var delivery PaymentWebhookDelivery
	var lastAttemptAt sql.NullTime
	var lastResponseStatus sql.NullInt64
	var deliveredAt sql.NullTime
	var status string
	var metadata string
	if err := row.Scan(
		&delivery.ID,
		&delivery.EventID,
		&delivery.EndpointID,
		&delivery.EndpointURL,
		&delivery.AppID,
		&delivery.Status,
		&delivery.AttemptCount,
		&delivery.NextAttemptAt,
		&lastAttemptAt,
		&lastResponseStatus,
		&delivery.LastError,
		&deliveredAt,
		&delivery.CreatedAt,
		&delivery.EventType,
		&delivery.Provider,
		&delivery.ProviderOrderID,
		&delivery.ProviderTransactionID,
		&delivery.PaymentOrder.ID,
		&delivery.PaymentOrder.AppID,
		&delivery.PaymentOrder.Provider,
		&delivery.PaymentOrder.ProviderOrderID,
		&delivery.PaymentOrder.ProviderTransactionID,
		&delivery.PaymentOrder.ExternalReference,
		&delivery.PaymentOrder.Amount,
		&delivery.PaymentOrder.Currency,
		&delivery.PaymentOrder.BuyerName,
		&delivery.PaymentOrder.BuyerEmail,
		&delivery.PaymentOrder.BuyerPhone,
		&status,
		&delivery.PaymentOrder.ProviderStatus,
		&metadata,
		&delivery.PaymentOrder.CreatedAt,
		&delivery.PaymentOrder.UpdatedAt,
	); err != nil {
		return PaymentWebhookDelivery{}, err
	}

	if lastAttemptAt.Valid {
		delivery.LastAttemptAt = &lastAttemptAt.Time
	}
	if lastResponseStatus.Valid {
		status := int(lastResponseStatus.Int64)
		delivery.LastResponseStatus = &status
	}
	if deliveredAt.Valid {
		delivery.DeliveredAt = &deliveredAt.Time
	}
	delivery.PaymentOrder.Status = provider.Status(status)
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &delivery.PaymentOrder.Metadata); err != nil {
			return PaymentWebhookDelivery{}, fmt.Errorf("decode payment delivery order metadata: %w", err)
		}
	}
	if delivery.PaymentOrder.Metadata == nil {
		delivery.PaymentOrder.Metadata = map[string]any{}
	}
	return delivery, nil
}

func scanPaymentWebhookDeliveryJob(row rowScanner) (PaymentWebhookDeliveryJob, error) {
	var job PaymentWebhookDeliveryJob
	var status string
	var metadata string
	if err := row.Scan(
		&job.ID,
		&job.EventID,
		&job.EndpointID,
		&job.AttemptCount,
		&job.EndpointURL,
		&job.AppID,
		&job.EventType,
		&job.Provider,
		&job.ProviderOrderID,
		&job.ProviderTransactionID,
		&job.ReceivedAt,
		&job.RawProviderPayload,
		&job.PaymentOrder.ID,
		&job.PaymentOrder.AppID,
		&job.PaymentOrder.Provider,
		&job.PaymentOrder.ProviderOrderID,
		&job.PaymentOrder.ProviderTransactionID,
		&job.PaymentOrder.ExternalReference,
		&job.PaymentOrder.Amount,
		&job.PaymentOrder.Currency,
		&job.PaymentOrder.BuyerName,
		&job.PaymentOrder.BuyerEmail,
		&job.PaymentOrder.BuyerPhone,
		&status,
		&job.PaymentOrder.ProviderStatus,
		&metadata,
		&job.PaymentOrder.CreatedAt,
		&job.PaymentOrder.UpdatedAt,
	); err != nil {
		return PaymentWebhookDeliveryJob{}, err
	}
	job.PaymentOrder.Status = provider.Status(status)
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &job.PaymentOrder.Metadata); err != nil {
			return PaymentWebhookDeliveryJob{}, fmt.Errorf("decode payment delivery order metadata: %w", err)
		}
	}
	if job.PaymentOrder.Metadata == nil {
		job.PaymentOrder.Metadata = map[string]any{}
	}
	return job, nil
}

func boundedLimitOffset(limit, offset, defaultLimit, maxLimit int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

const paymentProviderAccountColumns = `
	id::text, provider, name, environment, COALESCE(base_url, ''), credentials_encrypted,
	is_default, status, metadata::text, created_at, updated_at
`

func scanPaymentProviderAccount(row rowScanner) (PaymentProviderAccount, error) {
	var account PaymentProviderAccount
	var metadata string
	if err := row.Scan(
		&account.ID,
		&account.Provider,
		&account.Name,
		&account.Environment,
		&account.BaseURL,
		&account.CredentialsEncrypted,
		&account.IsDefault,
		&account.Status,
		&metadata,
		&account.CreatedAt,
		&account.UpdatedAt,
	); err != nil {
		return PaymentProviderAccount{}, err
	}
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &account.Metadata); err != nil {
			return PaymentProviderAccount{}, fmt.Errorf("decode payment provider account metadata: %w", err)
		}
	}
	if account.Metadata == nil {
		account.Metadata = map[string]any{}
	}
	return account, nil
}

func (r *PostgresRepository) CreateProviderAccount(ctx context.Context, providerKind, name, environment, baseURL, credentialsEncrypted string) (PaymentProviderAccount, error) {
	q := fmt.Sprintf(`
		INSERT INTO app.payment_provider_accounts (provider, name, environment, base_url, credentials_encrypted)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		RETURNING %s
	`, paymentProviderAccountColumns)

	row := r.db.QueryRowEx(ctx, q, nil, providerKind, name, environment, baseURL, credentialsEncrypted)
	account, err := scanPaymentProviderAccount(row)
	if err != nil {
		return PaymentProviderAccount{}, fmt.Errorf("create payment provider account: %w", err)
	}
	return account, nil
}

func (r *PostgresRepository) UpdateProviderAccount(ctx context.Context, id, name, environment, baseURL, status string, credentialsEncrypted *string) (PaymentProviderAccount, error) {
	q := fmt.Sprintf(`
		UPDATE app.payment_provider_accounts
		SET name = $2, environment = $3, base_url = NULLIF($4, ''), status = $5,
		    credentials_encrypted = COALESCE($6, credentials_encrypted)
		WHERE id = $1
		RETURNING %s
	`, paymentProviderAccountColumns)

	row := r.db.QueryRowEx(ctx, q, nil, id, name, environment, baseURL, status, credentialsEncrypted)
	account, err := scanPaymentProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentProviderAccount{}, ErrPaymentProviderAccountNotFound
	}
	if err != nil {
		return PaymentProviderAccount{}, fmt.Errorf("update payment provider account: %w", err)
	}
	return account, nil
}

func (r *PostgresRepository) DeleteProviderAccount(ctx context.Context, id string) error {
	tag, err := r.db.ExecEx(ctx, `DELETE FROM app.payment_provider_accounts WHERE id = $1`, nil, id)
	if err != nil {
		return fmt.Errorf("delete payment provider account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentProviderAccountNotFound
	}
	return nil
}

func (r *PostgresRepository) ListProviderAccounts(ctx context.Context) ([]PaymentProviderAccount, error) {
	q := fmt.Sprintf(`SELECT %s FROM app.payment_provider_accounts ORDER BY provider, environment, created_at DESC`, paymentProviderAccountColumns)

	rows, err := r.db.QueryEx(ctx, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list payment provider accounts: %w", err)
	}
	defer rows.Close()

	accounts := []PaymentProviderAccount{}
	for rows.Next() {
		account, err := scanPaymentProviderAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment provider account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment provider accounts: %w", err)
	}
	return accounts, nil
}

// SetDefaultProviderAccount unsets the previous default only within the
// target row's own (provider, environment) pair — the unique index backing
// is_default is scoped that way, so e.g. setting a new sonicpesa-production
// default must never clear a sonicpesa-sandbox or azampesa default.
func (r *PostgresRepository) SetDefaultProviderAccount(ctx context.Context, id string) error {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set default payment provider account: %w", err)
	}
	defer tx.Rollback()

	var providerKind, environment string
	err = tx.QueryRowEx(ctx, `SELECT provider, environment FROM app.payment_provider_accounts WHERE id = $1`, nil, id).Scan(&providerKind, &environment)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPaymentProviderAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup payment provider account: %w", err)
	}

	if _, err := tx.ExecEx(ctx, `UPDATE app.payment_provider_accounts SET is_default = false WHERE provider = $1 AND environment = $2 AND is_default`, nil, providerKind, environment); err != nil {
		return fmt.Errorf("unset default payment provider account: %w", err)
	}

	tag, err := tx.ExecEx(ctx, `UPDATE app.payment_provider_accounts SET is_default = true, status = 'active' WHERE id = $1`, nil, id)
	if err != nil {
		return fmt.Errorf("set default payment provider account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentProviderAccountNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit set default payment provider account: %w", err)
	}
	return nil
}

// GetDefaultProviderAccount resolves the active default account for a
// provider kind. A kind can have more than one default simultaneously (one
// per environment, per the partial unique index), so production is
// preferred when more than one matches.
func (r *PostgresRepository) GetDefaultProviderAccount(ctx context.Context, providerKind string) (PaymentProviderAccount, error) {
	q := fmt.Sprintf(`
		SELECT %s
		FROM app.payment_provider_accounts
		WHERE provider = $1 AND is_default AND status = 'active'
		ORDER BY (environment = 'production') DESC, updated_at DESC
		LIMIT 1
	`, paymentProviderAccountColumns)

	row := r.db.QueryRowEx(ctx, q, nil, providerKind)
	account, err := scanPaymentProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentProviderAccount{}, ErrPaymentProviderAccountNotFound
	}
	if err != nil {
		return PaymentProviderAccount{}, fmt.Errorf("get default payment provider account: %w", err)
	}
	return account, nil
}

// ---------- Ledger, balances, fees, withdrawals ----------

func (r *PostgresRepository) UpdateAppFees(ctx context.Context, appID string, input UpdateAppFeesInput) (PaymentApp, error) {
	const query = `
		UPDATE app.payment_apps
		SET fee_type = $2, fee_percent = $3::numeric, fee_fixed = $4::numeric, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, COALESCE(description, ''), status, fee_type, fee_percent::text, fee_fixed::text, created_at, updated_at
	`
	var app PaymentApp
	err := r.db.QueryRowEx(ctx, query, nil, appID, input.FeeType, input.FeePercent, input.FeeFixed).Scan(
		&app.ID, &app.Name, &app.Description, &app.Status, &app.FeeType, &app.FeePercent, &app.FeeFixed, &app.CreatedAt, &app.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentApp{}, ErrPaymentAppNotFound
	}
	if err != nil {
		return PaymentApp{}, fmt.Errorf("update payment app fees: %w", err)
	}
	return app, nil
}

// DeletePaymentApp is a soft delete — it sets status to 'deleted' rather
// than removing the row. app.payment_orders, app.payment_ledger_entries,
// and app.payment_app_members all reference payment_apps with ON DELETE
// CASCADE (migrations 000008/000023/000024), so a real DELETE here would
// permanently destroy that app's entire order/ledger/withdrawal history —
// never acceptable for a financial record. 'deleted' apps are excluded
// from ListPaymentApps/ListAppsForUser and already can't accept new orders
// via GetAppByAPIKeyHash, which only matches status = 'active'.
func (r *PostgresRepository) DeletePaymentApp(ctx context.Context, appID string) (PaymentApp, error) {
	const query = `
		UPDATE app.payment_apps
		SET status = 'deleted', updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, COALESCE(description, ''), status, fee_type, fee_percent::text, fee_fixed::text, created_at, updated_at
	`
	var app PaymentApp
	err := r.db.QueryRowEx(ctx, query, nil, appID).Scan(
		&app.ID, &app.Name, &app.Description, &app.Status, &app.FeeType, &app.FeePercent, &app.FeeFixed, &app.CreatedAt, &app.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentApp{}, ErrPaymentAppNotFound
	}
	if err != nil {
		return PaymentApp{}, fmt.Errorf("delete payment app: %w", err)
	}
	return app, nil
}

// GetAppBalance derives every figure from the ledger (never a stored
// column) plus a separate read-only total of still-unsettled orders.
func (r *PostgresRepository) GetAppBalance(ctx context.Context, appID string) (AppBalance, error) {
	const ledgerQuery = `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE -amount END), 0)::text,
			COALESCE(SUM(CASE WHEN entry_type = 'payment_credit' THEN amount ELSE 0 END), 0)::text,
			COALESCE(SUM(CASE WHEN entry_type = 'platform_fee_debit' THEN amount ELSE 0 END), 0)::text,
			COALESCE(SUM(CASE WHEN entry_type = 'withdrawal_debit' THEN amount
			              WHEN entry_type = 'withdrawal_reversal_credit' THEN -amount ELSE 0 END), 0)::text,
			COALESCE((SELECT currency FROM app.payment_ledger_entries WHERE app_id = $1::uuid ORDER BY created_at DESC LIMIT 1), 'TZS'),
			COALESCE(SUM(CASE WHEN created_at <= $2 THEN (CASE WHEN direction = 'credit' THEN amount ELSE -amount END) ELSE 0 END), 0)::text
		FROM app.payment_ledger_entries
		WHERE app_id = $1::uuid
	`

	var balance AppBalance
	balance.AppID = appID
	sevenDaysAgo := time.Now().UTC().AddDate(0, 0, -7)
	if err := r.db.QueryRowEx(ctx, ledgerQuery, nil, appID, sevenDaysAgo).Scan(
		&balance.AvailableBalance, &balance.TotalRevenue, &balance.TotalPlatformFees, &balance.TotalWithdrawn, &balance.Currency,
		&balance.AvailableBalanceSevenDaysAgo,
	); err != nil {
		return AppBalance{}, fmt.Errorf("compute app balance: %w", err)
	}

	const pendingQuery = `
		SELECT COALESCE(SUM(amount), 0)::text
		FROM app.payment_orders
		WHERE app_id = $1::uuid AND status IN ('pending', 'processing')
	`
	if err := r.db.QueryRowEx(ctx, pendingQuery, nil, appID).Scan(&balance.PendingOrderTotal); err != nil {
		return AppBalance{}, fmt.Errorf("compute pending order total: %w", err)
	}

	return balance, nil
}

func (r *PostgresRepository) ListLedgerEntries(ctx context.Context, filter LedgerEntryListFilter) (LedgerEntryListResult, error) {
	limit, offset := boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)

	var total int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.payment_ledger_entries WHERE ($1 = '' OR app_id::text = $1)`, nil, filter.AppID).Scan(&total); err != nil {
		return LedgerEntryListResult{}, fmt.Errorf("count ledger entries: %w", err)
	}

	const query = `
		SELECT id::text, app_id::text, COALESCE(payment_order_id::text, ''), COALESCE(withdrawal_id::text, ''),
		       entry_type, direction, amount::text, currency, description, COALESCE(created_by, ''), created_at
		FROM app.payment_ledger_entries
		WHERE ($1 = '' OR app_id::text = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryEx(ctx, query, nil, filter.AppID, limit, offset)
	if err != nil {
		return LedgerEntryListResult{}, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	entries := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.AppID, &e.PaymentOrderID, &e.WithdrawalID, &e.EntryType, &e.Direction, &e.Amount, &e.Currency, &e.Description, &e.CreatedBy, &e.CreatedAt); err != nil {
			return LedgerEntryListResult{}, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return LedgerEntryListResult{}, fmt.Errorf("iterate ledger entries: %w", err)
	}

	return LedgerEntryListResult{Items: entries, Total: total, Limit: limit, Offset: offset}, nil
}

func scanWithdrawal(row interface {
	Scan(dest ...interface{}) error
}) (PaymentWithdrawal, error) {
	var w PaymentWithdrawal
	var destinationDetails []byte
	var dispatchedAt sql.NullTime
	if err := row.Scan(
		&w.ID, &w.AppID, &w.Amount, &w.Currency, &w.DestinationType, &destinationDetails,
		&w.Status, &w.RequestedBy, &w.ApprovedBy, &w.Notes,
		&w.Provider, &w.ProviderPayoutID, &w.ProviderStatus, &w.FailureReason, &dispatchedAt,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return PaymentWithdrawal{}, err
	}
	if len(destinationDetails) > 0 {
		if err := json.Unmarshal(destinationDetails, &w.DestinationDetails); err != nil {
			return PaymentWithdrawal{}, fmt.Errorf("unmarshal withdrawal destination details: %w", err)
		}
	}
	if dispatchedAt.Valid {
		w.DispatchedAt = &dispatchedAt.Time
	}
	return w, nil
}

const withdrawalColumns = `
	id::text, app_id::text, amount::text, currency, destination_type, destination_details::text,
	status, requested_by, COALESCE(approved_by, ''), notes,
	COALESCE(provider, ''), COALESCE(provider_payout_id, ''), COALESCE(provider_status, ''), failure_reason, dispatched_at,
	created_at, updated_at
`

func (r *PostgresRepository) CreateWithdrawal(ctx context.Context, input CreateWithdrawalInput) (PaymentWithdrawal, error) {
	details, err := marshalMetadata(input.DestinationDetails)
	if err != nil {
		return PaymentWithdrawal{}, err
	}

	q := fmt.Sprintf(`
		INSERT INTO app.payment_withdrawals (app_id, amount, currency, destination_type, destination_details, requested_by, notes)
		VALUES ($1::uuid, $2::numeric, $3, $4, $5::jsonb, $6, $7)
		RETURNING %s
	`, withdrawalColumns)

	row := r.db.QueryRowEx(ctx, q, nil, input.AppID, input.Amount, input.Currency, input.DestinationType, string(details), input.RequestedBy, input.Notes)
	return scanWithdrawal(row)
}

func (r *PostgresRepository) ListWithdrawals(ctx context.Context, filter WithdrawalListFilter) (WithdrawalListResult, error) {
	limit, offset := boundedLimitOffset(filter.Limit, filter.Offset, 50, 200)

	var total int64
	if err := r.db.QueryRowEx(ctx, `
		SELECT COUNT(*) FROM app.payment_withdrawals
		WHERE ($1 = '' OR app_id::text = $1) AND ($2 = '' OR status = $2)
	`, nil, filter.AppID, filter.Status).Scan(&total); err != nil {
		return WithdrawalListResult{}, fmt.Errorf("count withdrawals: %w", err)
	}

	q := fmt.Sprintf(`
		SELECT %s
		FROM app.payment_withdrawals
		WHERE ($1 = '' OR app_id::text = $1) AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, withdrawalColumns)
	rows, err := r.db.QueryEx(ctx, q, nil, filter.AppID, filter.Status, limit, offset)
	if err != nil {
		return WithdrawalListResult{}, fmt.Errorf("list withdrawals: %w", err)
	}
	defer rows.Close()

	withdrawals := []PaymentWithdrawal{}
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return WithdrawalListResult{}, fmt.Errorf("scan withdrawal: %w", err)
		}
		withdrawals = append(withdrawals, w)
	}
	if err := rows.Err(); err != nil {
		return WithdrawalListResult{}, fmt.Errorf("iterate withdrawals: %w", err)
	}

	return WithdrawalListResult{Items: withdrawals, Total: total, Limit: limit, Offset: offset}, nil
}

func (r *PostgresRepository) GetWithdrawalByID(ctx context.Context, id string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`SELECT %s FROM app.payment_withdrawals WHERE id::text = $1`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("get withdrawal: %w", err)
	}
	return w, nil
}

// ApproveWithdrawal locks the owning app row for the duration of the
// transaction (a mutex over that app's ledger), re-sums the balance inside
// the same transaction, and only then debits it — this is what prevents two
// concurrent approvals from both reading a stale balance and over-paying.
func (r *PostgresRepository) ApproveWithdrawal(ctx context.Context, id, approvedBy string) (PaymentWithdrawal, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("begin approve withdrawal transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	q := fmt.Sprintf(`SELECT %s FROM app.payment_withdrawals WHERE id::text = $1 FOR UPDATE`, withdrawalColumns)
	var withdrawal PaymentWithdrawal
	withdrawal, err = scanWithdrawal(tx.QueryRowEx(ctx, q, nil, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("lock withdrawal: %w", err)
	}
	if withdrawal.Status != WithdrawalStatusRequested {
		err = ErrInvalidWithdrawalTransition
		return PaymentWithdrawal{}, err
	}

	// Lock the app row as a mutex over its ledger for the rest of this transaction.
	if _, err = tx.ExecEx(ctx, `SELECT 1 FROM app.payment_apps WHERE id = $1::uuid FOR UPDATE`, nil, withdrawal.AppID); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("lock payment app: %w", err)
	}

	var available string
	if err = tx.QueryRowEx(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE -amount END), 0)::text
		FROM app.payment_ledger_entries WHERE app_id = $1::uuid
	`, nil, withdrawal.AppID).Scan(&available); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("compute balance for withdrawal approval: %w", err)
	}

	availableRat, availOk := new(big.Rat).SetString(available)
	amountRat, amountOk := new(big.Rat).SetString(withdrawal.Amount)
	if !availOk || !amountOk {
		err = fmt.Errorf("parse withdrawal amounts: available=%q amount=%q", available, withdrawal.Amount)
		return PaymentWithdrawal{}, err
	}
	if amountRat.Cmp(availableRat) > 0 {
		err = ErrInsufficientBalance
		return PaymentWithdrawal{}, err
	}

	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.payment_ledger_entries (app_id, withdrawal_id, entry_type, direction, amount, currency, description, created_by)
		VALUES ($1::uuid, $2::uuid, 'withdrawal_debit', 'debit', $3::numeric, $4, 'Withdrawal approved', $5)
	`, nil, withdrawal.AppID, withdrawal.ID, withdrawal.Amount, withdrawal.Currency, approvedBy); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("insert withdrawal debit ledger entry: %w", err)
	}

	q = fmt.Sprintf(`
		UPDATE app.payment_withdrawals SET status = 'approved', approved_by = $2, updated_at = NOW()
		WHERE id::text = $1
		RETURNING %s
	`, withdrawalColumns)
	withdrawal, err = scanWithdrawal(tx.QueryRowEx(ctx, q, nil, id, approvedBy))
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("update withdrawal to approved: %w", err)
	}

	if err = tx.CommitEx(ctx); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("commit approve withdrawal transaction: %w", err)
	}
	return withdrawal, nil
}

func (r *PostgresRepository) RejectWithdrawal(ctx context.Context, id string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`
		UPDATE app.payment_withdrawals SET status = 'rejected', updated_at = NOW()
		WHERE id::text = $1 AND status = 'requested'
		RETURNING %s
	`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, id))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetWithdrawalByID(ctx, id); getErr == nil {
			return PaymentWithdrawal{}, ErrInvalidWithdrawalTransition
		}
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("reject withdrawal: %w", err)
	}
	return w, nil
}

func (r *PostgresRepository) MarkWithdrawalPaid(ctx context.Context, id string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`
		UPDATE app.payment_withdrawals SET status = 'paid', updated_at = NOW()
		WHERE id::text = $1 AND status IN ('approved', 'processing')
		RETURNING %s
	`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, id))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetWithdrawalByID(ctx, id); getErr == nil {
			return PaymentWithdrawal{}, ErrInvalidWithdrawalTransition
		}
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("mark withdrawal paid: %w", err)
	}
	return w, nil
}

// MarkWithdrawalFailed reverses the debit posted at approval time with a
// withdrawal_reversal_credit, restoring the app's balance.
func (r *PostgresRepository) MarkWithdrawalFailed(ctx context.Context, id, notes string) (PaymentWithdrawal, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("begin mark withdrawal failed transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	q := fmt.Sprintf(`SELECT %s FROM app.payment_withdrawals WHERE id::text = $1 FOR UPDATE`, withdrawalColumns)
	var withdrawal PaymentWithdrawal
	withdrawal, err = scanWithdrawal(tx.QueryRowEx(ctx, q, nil, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("lock withdrawal: %w", err)
	}
	if withdrawal.Status != WithdrawalStatusApproved && withdrawal.Status != WithdrawalStatusProcessing {
		err = ErrInvalidWithdrawalTransition
		return PaymentWithdrawal{}, err
	}

	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.payment_ledger_entries (app_id, withdrawal_id, entry_type, direction, amount, currency, description)
		VALUES ($1::uuid, $2::uuid, 'withdrawal_reversal_credit', 'credit', $3::numeric, $4, $5)
	`, nil, withdrawal.AppID, withdrawal.ID, withdrawal.Amount, withdrawal.Currency, "Withdrawal failed — reversed: "+notes); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("insert withdrawal reversal ledger entry: %w", err)
	}

	q = fmt.Sprintf(`
		UPDATE app.payment_withdrawals SET status = 'failed', notes = $2, updated_at = NOW()
		WHERE id::text = $1
		RETURNING %s
	`, withdrawalColumns)
	withdrawal, err = scanWithdrawal(tx.QueryRowEx(ctx, q, nil, id, notes))
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("update withdrawal to failed: %w", err)
	}

	if err = tx.CommitEx(ctx); err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("commit mark withdrawal failed transaction: %w", err)
	}
	return withdrawal, nil
}

// DispatchWithdrawal records that a withdrawal has been sent to a payout
// provider. The "AND provider_payout_id IS NULL" guard (alongside the
// unique index on (provider, provider_payout_id) from migration 000027) is
// what makes dispatch idempotent — a withdrawal can only ever be recorded
// as dispatched once, so retrying AttemptPayout after this succeeds is
// always a safe no-op rather than a second payout.
func (r *PostgresRepository) DispatchWithdrawal(ctx context.Context, id, provider, providerPayoutID, status, providerStatus string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`
		UPDATE app.payment_withdrawals
		SET provider = $2, provider_payout_id = $3, status = $4, provider_status = $5, dispatched_at = NOW(), updated_at = NOW()
		WHERE id::text = $1 AND status = 'approved' AND provider_payout_id IS NULL
		RETURNING %s
	`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, id, provider, providerPayoutID, status, providerStatus))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetWithdrawalByID(ctx, id); getErr == nil {
			return PaymentWithdrawal{}, ErrWithdrawalAlreadyDispatched
		}
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("dispatch withdrawal: %w", err)
	}
	return w, nil
}

// RecordPayoutFailure records that a Disburse call itself failed (network
// error, provider rejected the request before ever creating a payout) —
// no provider_payout_id exists to dispatch, so status stays 'approved' and
// only failure_reason is set. This is what lets AdminPaymentWithdrawals
// show a "Retry payout" action instead of silently losing the error.
func (r *PostgresRepository) RecordPayoutFailure(ctx context.Context, id, provider, reason string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`
		UPDATE app.payment_withdrawals
		SET provider = $2, failure_reason = $3, updated_at = NOW()
		WHERE id::text = $1 AND status = 'approved'
		RETURNING %s
	`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, id, provider, reason))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetWithdrawalByID(ctx, id); getErr == nil {
			return PaymentWithdrawal{}, ErrInvalidWithdrawalTransition
		}
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("record payout failure: %w", err)
	}
	return w, nil
}

func (r *PostgresRepository) GetWithdrawalByProviderPayoutID(ctx context.Context, provider, providerPayoutID string) (PaymentWithdrawal, error) {
	q := fmt.Sprintf(`SELECT %s FROM app.payment_withdrawals WHERE provider = $1 AND provider_payout_id = $2`, withdrawalColumns)
	w, err := scanWithdrawal(r.db.QueryRowEx(ctx, q, nil, provider, providerPayoutID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return PaymentWithdrawal{}, fmt.Errorf("get withdrawal by provider payout id: %w", err)
	}
	return w, nil
}

func (r *PostgresRepository) ListPayoutReconciliationCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]PaymentWithdrawal, error) {
	if limit <= 0 {
		limit = 50
	}

	q := fmt.Sprintf(`
		SELECT %s FROM app.payment_withdrawals
		WHERE status = 'processing'
		  AND provider_payout_id IS NOT NULL
		  AND dispatched_at <= $1
		ORDER BY dispatched_at ASC
		LIMIT $2
	`, withdrawalColumns)

	rows, err := r.db.QueryEx(ctx, q, nil, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list payout reconciliation candidates: %w", err)
	}
	defer rows.Close()

	withdrawals := []PaymentWithdrawal{}
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payout reconciliation candidate: %w", err)
		}
		withdrawals = append(withdrawals, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payout reconciliation candidates: %w", err)
	}
	return withdrawals, nil
}

// ---------- Merchant identity & access (Phase 2) ----------

func (r *PostgresRepository) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
	const query = `SELECT id::text FROM app.users WHERE lower(email) = lower($1) LIMIT 1`
	var id string
	err := r.db.QueryRowEx(ctx, query, nil, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find user by email: %w", err)
	}
	return id, nil
}

func (r *PostgresRepository) AddAppMember(ctx context.Context, appID, userID, addedBy string) (AppMember, error) {
	const query = `
		INSERT INTO app.payment_app_members (app_id, user_id, added_by)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text, app_id::text, user_id::text, added_by, created_at
	`
	var m AppMember
	err := r.db.QueryRowEx(ctx, query, nil, appID, userID, addedBy).Scan(&m.ID, &m.AppID, &m.UserID, &m.AddedBy, &m.CreatedAt)
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return AppMember{}, ErrAlreadyMember
		}
		return AppMember{}, fmt.Errorf("add app member: %w", err)
	}
	return m, nil
}

// ListAppMembers joins the service-owned user table for display fields.
func (r *PostgresRepository) ListAppMembers(ctx context.Context, appID string) ([]AppMember, error) {
	const query = `
		SELECT m.id::text, m.app_id::text, m.user_id::text,
		       u.email, u.full_name, m.added_by, m.created_at, u.phone
		FROM app.payment_app_members m
		JOIN app.users u ON u.id = m.user_id
		WHERE m.app_id = $1::uuid
		ORDER BY m.created_at ASC
	`
	rows, err := r.db.QueryEx(ctx, query, nil, appID)
	if err != nil {
		return nil, fmt.Errorf("list app members: %w", err)
	}
	defer rows.Close()

	members := []AppMember{}
	for rows.Next() {
		var m AppMember
		if err := rows.Scan(&m.ID, &m.AppID, &m.UserID, &m.Email, &m.FullName, &m.AddedBy, &m.CreatedAt, &m.Phone); err != nil {
			return nil, fmt.Errorf("scan app member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app members: %w", err)
	}
	return members, nil
}

func (r *PostgresRepository) RemoveAppMember(ctx context.Context, appID, userID string) error {
	tag, err := r.db.ExecEx(ctx, `DELETE FROM app.payment_app_members WHERE app_id = $1::uuid AND user_id = $2::uuid`, nil, appID, userID)
	if err != nil {
		return fmt.Errorf("remove app member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAppMemberNotFound
	}
	return nil
}

// IsAppMember is the access-check primitive Phase 3's merchant-facing routes
// will call directly (no generic middleware, since the app id comes from
// the path and varies per route — each handler checks membership itself,
// the same way admin handlers already read r.PathValue("id") themselves).
func (r *PostgresRepository) IsAppMember(ctx context.Context, userID, appID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM app.payment_app_members WHERE user_id = $1::uuid AND app_id = $2::uuid)`
	var exists bool
	if err := r.db.QueryRowEx(ctx, query, nil, userID, appID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check app membership: %w", err)
	}
	return exists, nil
}

// ListAppsForUser is the merchant-facing counterpart to ListPaymentApps —
// only the apps the given user is a member of, via payment_app_members.
func (r *PostgresRepository) ListAppsForUser(ctx context.Context, userID string) ([]PaymentApp, error) {
	const query = `
		SELECT a.id::text, a.name, COALESCE(a.description, ''), a.status,
		       a.fee_type, a.fee_percent::text, a.fee_fixed::text, a.created_at, a.updated_at
		FROM app.payment_apps a
		JOIN app.payment_app_members m ON m.app_id = a.id
		WHERE m.user_id = $1::uuid AND a.status != 'deleted'
		ORDER BY a.name ASC
	`
	rows, err := r.db.QueryEx(ctx, query, nil, userID)
	if err != nil {
		return nil, fmt.Errorf("list apps for user: %w", err)
	}
	defer rows.Close()

	apps := []PaymentApp{}
	for rows.Next() {
		var app PaymentApp
		if err := rows.Scan(&app.ID, &app.Name, &app.Description, &app.Status, &app.FeeType, &app.FeePercent, &app.FeeFixed, &app.CreatedAt, &app.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan app for user: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate apps for user: %w", err)
	}
	return apps, nil
}

// ---------- Refunds & reversals (Phase 4) ----------

// executor is satisfied by both *pgx.Tx and *pgx.ConnPool (their ExecEx/
// QueryRowEx signatures are identical) — lets postRefundEntries run either
// inside an existing transaction (the automatic webhook path) or its own
// (the manual admin path) without duplicating the insertion logic.
type executor interface {
	ExecEx(ctx context.Context, sql string, options *pgx.QueryExOptions, arguments ...interface{}) (pgx.CommandTag, error)
	QueryRowEx(ctx context.Context, sql string, options *pgx.QueryExOptions, args ...interface{}) *pgx.Row
}

// postRefundEntries reverses the original payment_credit and (if any)
// platform_fee_debit recorded for an order at settlement time — never
// recalculated from the app's current fee config, since fees can change
// after the fact. Idempotent: a no-op if a refund_debit already exists for
// this order, protecting against a replayed webhook or a duplicate admin
// action from double-reversing the same payment.
func postRefundEntries(ctx context.Context, tx executor, appID, orderID string) error {
	var alreadyRefunded bool
	if err := tx.QueryRowEx(ctx, `
		SELECT EXISTS (SELECT 1 FROM app.payment_ledger_entries WHERE payment_order_id = $1::uuid AND entry_type = 'refund_debit')
	`, nil, orderID).Scan(&alreadyRefunded); err != nil {
		return fmt.Errorf("check existing refund: %w", err)
	}
	if alreadyRefunded {
		return nil
	}

	var creditAmount, currency string
	err := tx.QueryRowEx(ctx, `
		SELECT amount::text, currency FROM app.payment_ledger_entries
		WHERE payment_order_id = $1::uuid AND entry_type = 'payment_credit'
		LIMIT 1
	`, nil, orderID).Scan(&creditAmount, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing was ever credited for this order (e.g. it settled before
		// the ledger existed and was never backfilled) — nothing to reverse.
		return nil
	}
	if err != nil {
		return fmt.Errorf("load original payment credit: %w", err)
	}

	if _, err := tx.ExecEx(ctx, `
		INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, description)
		VALUES ($1::uuid, $2::uuid, 'refund_debit', 'debit', $3::numeric, $4, 'Payment refunded/reversed')
		ON CONFLICT DO NOTHING
	`, nil, appID, orderID, creditAmount, currency); err != nil {
		return fmt.Errorf("insert refund debit ledger entry: %w", err)
	}

	var feeAmount string
	err = tx.QueryRowEx(ctx, `
		SELECT amount::text FROM app.payment_ledger_entries
		WHERE payment_order_id = $1::uuid AND entry_type = 'platform_fee_debit'
		LIMIT 1
	`, nil, orderID).Scan(&feeAmount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load original platform fee: %w", err)
	}
	if err == nil && feeAmount != "" && feeAmount != "0.00" {
		if _, err := tx.ExecEx(ctx, `
			INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, description)
			VALUES ($1::uuid, $2::uuid, 'refund_fee_reversal_credit', 'credit', $3::numeric, $4, 'Platform fee reversed on refund')
			ON CONFLICT DO NOTHING
		`, nil, appID, orderID, feeAmount, currency); err != nil {
			return fmt.Errorf("insert refund fee reversal ledger entry: %w", err)
		}
	}

	return nil
}

// RefundOrder is the manual/admin fallback for reversing a paid order —
// used when no provider webhook arrives to drive it automatically (see
// PostRefund in ApplyWebhookEvent for the automatic path). Locks the order
// row for the duration of the transaction so it can't race a concurrent
// webhook reversing the same order.
func (r *PostgresRepository) RefundOrder(ctx context.Context, orderID, source string) (PaymentOrder, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("begin refund order transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	q := paymentOrderSelect + ` WHERE id = $1::uuid FOR UPDATE`
	var order PaymentOrder
	order, err = scanPaymentOrder(tx.QueryRowEx(ctx, q, nil, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentOrder{}, ErrPaymentOrderNotFound
	}
	if err != nil {
		return PaymentOrder{}, fmt.Errorf("lock payment order: %w", err)
	}
	if order.Status != provider.StatusPaid {
		err = ErrOrderNotRefundable
		return PaymentOrder{}, err
	}

	var alreadyRefunded bool
	if err = tx.QueryRowEx(ctx, `
		SELECT EXISTS (SELECT 1 FROM app.payment_ledger_entries WHERE payment_order_id = $1::uuid AND entry_type = 'refund_debit')
	`, nil, orderID).Scan(&alreadyRefunded); err != nil {
		return PaymentOrder{}, fmt.Errorf("check existing refund: %w", err)
	}
	if alreadyRefunded {
		err = ErrAlreadyRefunded
		return PaymentOrder{}, err
	}

	if order.AppID != "" {
		if err = postRefundEntries(ctx, tx, order.AppID, order.ID); err != nil {
			return PaymentOrder{}, fmt.Errorf("post refund ledger entries: %w", err)
		}
	}

	const updateOrder = `
		UPDATE app.payment_orders SET status = 'reversed', updated_at = NOW() WHERE id = $1::uuid
	`
	if _, err = tx.ExecEx(ctx, updateOrder, nil, orderID); err != nil {
		return PaymentOrder{}, fmt.Errorf("update order to reversed: %w", err)
	}

	const insertHistory = `
		INSERT INTO app.payment_status_history (payment_order_id, from_status, to_status, provider_status, source)
		VALUES ($1::uuid, $2, 'reversed', $3, $4)
	`
	if _, err = tx.ExecEx(ctx, insertHistory, nil, orderID, string(order.Status), order.ProviderStatus, source); err != nil {
		return PaymentOrder{}, fmt.Errorf("insert refund status history: %w", err)
	}

	if err = tx.CommitEx(ctx); err != nil {
		return PaymentOrder{}, fmt.Errorf("commit refund order transaction: %w", err)
	}

	order.Status = provider.StatusReversed
	return order, nil
}
