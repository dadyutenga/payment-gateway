package payments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"azsubay-payments-gateway/internal/modules/payments/provider"
	azcrypto "azsubay-payments-gateway/internal/platform/crypto"

	"github.com/jackc/pgx"
)

// testCipher and testEncryptedCredentials back fakePaymentRepository's
// default GetDefaultProviderAccount response, so resolveProvider's
// decrypt-then-construct path works the same way it does against real
// admin-configured provider accounts.
var testCipher = func() *azcrypto.Cipher {
	c, err := azcrypto.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		panic(err)
	}
	return c
}()

var testEncryptedCredentials = func() string {
	encrypted, err := testCipher.Encrypt("{}")
	if err != nil {
		panic(err)
	}
	return encrypted
}()

// registryFor builds a provider.Constructor registry keyed by each fake
// provider's name, standing in for providers.Registry in tests.
func registryFor(fakes ...*fakePaymentProvider) map[string]provider.Constructor {
	registry := make(map[string]provider.Constructor, len(fakes))
	for _, f := range fakes {
		f := f
		registry[f.name] = func(_ string, _ map[string]string) provider.PaymentProvider { return f }
	}
	return registry
}

type fakePaymentProvider struct {
	name         string
	verifyErr    error
	event        provider.WebhookEvent
	parseErr     error
	createReq    provider.CreateOrderRequest
	createResp   provider.ProviderOrder
	createErr    error
	statusResp   provider.ProviderStatus
	statusErr    error
	createCalled int
	statusCalled int

	disburseReq        provider.DisburseRequest
	disburseResp       provider.DisburseResult
	disburseErr        error
	disburseCalled     int
	payoutStatusResp   provider.DisburseResult
	payoutStatusErr    error
	payoutStatusCalled int

	refundCreds  map[string]string
	refundRef    string
	refundAmount string
	refundCurr   string
	refundReason string
	refundResp   provider.ProviderRefundResult
	refundErr    error
	refundCalled int
}

// RefundOrder makes fakePaymentProvider satisfy provider.Refunder so refund
// tests can exercise the provider-integrated path without a real call.
func (f *fakePaymentProvider) RefundOrder(_ context.Context, creds map[string]string, ref, amount, currency, reason string) (provider.ProviderRefundResult, error) {
	f.refundCalled++
	f.refundCreds = creds
	f.refundRef = ref
	f.refundAmount = amount
	f.refundCurr = currency
	f.refundReason = reason
	return f.refundResp, f.refundErr
}

// Disburse and CheckPayoutStatus make fakePaymentProvider satisfy
// provider.Disburser so AttemptPayout/ReconcilePayouts tests can exercise
// the automated payout path without a real SonicPesa call.
func (f *fakePaymentProvider) Disburse(_ context.Context, req provider.DisburseRequest) (provider.DisburseResult, error) {
	f.disburseCalled++
	f.disburseReq = req
	return f.disburseResp, f.disburseErr
}

func (f *fakePaymentProvider) CheckPayoutStatus(_ context.Context, _ string) (provider.DisburseResult, error) {
	f.payoutStatusCalled++
	return f.payoutStatusResp, f.payoutStatusErr
}

func (f *fakePaymentProvider) Name() string {
	return f.name
}

func (f *fakePaymentProvider) VerifyWebhook(_ http.Header, _ []byte) error {
	return f.verifyErr
}

func (f *fakePaymentProvider) ParseWebhook(_ []byte) (provider.WebhookEvent, error) {
	return f.event, f.parseErr
}

func (f *fakePaymentProvider) CreateOrder(_ context.Context, req provider.CreateOrderRequest) (provider.ProviderOrder, error) {
	f.createCalled++
	f.createReq = req
	return f.createResp, f.createErr
}

func (f *fakePaymentProvider) CheckOrderStatus(_ context.Context, _ string) (provider.ProviderStatus, error) {
	f.statusCalled++
	return f.statusResp, f.statusErr
}

type fakePaymentRepository struct {
	duplicate               bool
	order                   PaymentOrder
	orderErr                error
	referenceOrder          PaymentOrder
	referenceErr            error
	appListResult           PaymentAppListResult
	createdOrder            PaymentOrder
	createOrderInput        CreatePaymentOrderRepositoryInput
	createOrderInputs       []CreatePaymentOrderRepositoryInput
	createOrderErr          error
	failCreateOnce          bool
	updatedProviderOrder    PaymentOrder
	updateProviderErr       error
	updateProviderCalls     []updateProviderDetailsCall
	getOrder                PaymentOrder
	getOrderErr             error
	getEvent                PaymentEvent
	getEventErr             error
	claimRefundInputs       []ClaimRefundInput
	claimRefundErr          error
	claimedRefund           PaymentRefund
	attachedRefundIDs       map[string]string
	attachRefundErr         error
	confirmRefundCalls      []confirmRefundCall
	confirmRefundErr        error
	failRefundCalls         []failRefundCall
	failRefundErr           error
	reconcileCandidates     []PaymentOrder
	expireCandidates        []PaymentOrder
	expireErr               error
	idempotencyRecords      map[idempotencyScope]IdempotencyRecord
	idempotencyErr          error
	expiredIdempotencyCount int64
	searchFilter            PaymentOrderSearchFilter
	searchResult            PaymentOrderSearchResult
	eventFilter             PaymentEventListFilter
	eventResult             PaymentEventListResult
	deliveryFilter          PaymentWebhookDeliveryListFilter
	deliveryResult          PaymentWebhookDeliveryListResult
	replayedDeliveryID      string
	metricsStaleBefore      time.Time
	metrics                 PaymentMetrics
	updatedOrder            PaymentOrder
	events                  []PaymentEventInput
	marked                  []string
	applied                 []ApplyWebhookEventInput
	orderLookups            int
	endpointInput           CreatePaymentWebhookEndpointRepositoryInput
	endpoint                PaymentWebhookEndpoint
	createdDeliveries       int64
	deliveryForEventCalls   []deliveryForEventCall
	replayedEventID         string
	replayedEventCount      int64
	deliveryJobs            []PaymentWebhookDeliveryJob
	recordedAttempts        []RecordWebhookDeliveryAttemptInput
	recordCtxWasCancelled   []bool
	providerAccount         PaymentProviderAccount
	providerAccountErr      error

	withdrawal                     PaymentWithdrawal
	withdrawalErr                  error
	payoutClaimCalls               []payoutClaimCall
	payoutClaimErr                 error
	dispatchCalls                  []dispatchWithdrawalCall
	dispatchErr                    error
	recordPayoutFailureCalls       []recordPayoutFailureCall
	recordPayoutFailureErr         error
	payoutWithdrawal               PaymentWithdrawal
	payoutWithdrawalErr            error
	payoutReconciliationCandidates []PaymentWithdrawal
	appMembers                     []AppMember
	deletePaymentAppErr            error
}

type dispatchWithdrawalCall struct {
	id               string
	provider         string
	providerPayoutID string
	status           string
	providerStatus   string
}

type recordPayoutFailureCall struct {
	id       string
	provider string
	reason   string
}

func (r *fakePaymentRepository) GetAppByAPIKeyHash(_ context.Context, _ string) (PaymentApp, error) {
	return PaymentApp{ID: "app_test", Name: "Test App", Status: "active"}, nil
}

func (r *fakePaymentRepository) CreatePaymentApp(_ context.Context, input CreatePaymentAppInput) (PaymentApp, error) {
	return PaymentApp{ID: "app_test", Name: input.Name, Description: input.Description, Status: "active"}, nil
}

func (r *fakePaymentRepository) CreatePaymentAPIKey(_ context.Context, _, _ string) error {
	return nil
}

func (r *fakePaymentRepository) RevokePaymentAPIKeys(_ context.Context, _ string) error {
	return nil
}

func (r *fakePaymentRepository) ListPaymentApps(_ context.Context, limit, offset int) (PaymentAppListResult, error) {
	if r.appListResult.Limit == 0 {
		r.appListResult.Limit = limit
		r.appListResult.Offset = offset
	}
	return r.appListResult, nil
}

func (r *fakePaymentRepository) CreatePaymentOrder(_ context.Context, input CreatePaymentOrderRepositoryInput) (PaymentOrder, error) {
	r.createOrderInput = input
	r.createOrderInputs = append(r.createOrderInputs, input)
	if r.failCreateOnce {
		// Simulate losing the pending-insert race: the winner's row is now
		// visible to the conflicting lookup.
		r.failCreateOnce = false
		r.referenceErr = nil
		return PaymentOrder{}, pgx.PgError{Code: "23505"}
	}
	if r.createOrderErr != nil {
		return PaymentOrder{}, r.createOrderErr
	}
	if r.createdOrder.ID != "" {
		return r.createdOrder, nil
	}
	return PaymentOrder{
		ID:                    "pay_test",
		AppID:                 input.AppID,
		Provider:              input.Provider,
		ProviderOrderID:       input.ProviderOrderID,
		ProviderTransactionID: input.ProviderTransactionID,
		ExternalReference:     input.ExternalReference,
		Amount:                input.Amount,
		Currency:              input.Currency,
		BuyerName:             input.BuyerName,
		BuyerEmail:            input.BuyerEmail,
		BuyerPhone:            input.BuyerPhone,
		Status:                input.Status,
		ProviderStatus:        input.ProviderStatus,
		Metadata:              input.Metadata,
	}, nil
}

type updateProviderDetailsCall struct {
	id                    string
	providerOrderID       string
	providerTransactionID string
	status                provider.Status
	providerStatus        string
}

func (r *fakePaymentRepository) UpdatePaymentOrderProviderDetails(_ context.Context, id, providerOrderID, providerTransactionID string, status provider.Status, providerStatus string, metadata map[string]any) (PaymentOrder, error) {
	r.updateProviderCalls = append(r.updateProviderCalls, updateProviderDetailsCall{id, providerOrderID, providerTransactionID, status, providerStatus})
	if r.updateProviderErr != nil {
		return PaymentOrder{}, r.updateProviderErr
	}
	if r.updatedProviderOrder.ID != "" {
		return r.updatedProviderOrder, nil
	}
	order := r.createdOrder
	if order.ID == "" {
		order.ID = "pay_test"
	}
	order.ProviderOrderID = providerOrderID
	order.ProviderTransactionID = providerTransactionID
	order.Status = status
	order.ProviderStatus = providerStatus
	if metadata != nil {
		order.Metadata = metadata
	}
	return order, nil
}

func (r *fakePaymentRepository) ClaimWithdrawalForPayout(_ context.Context, id, providerName string) (PaymentWithdrawal, error) {
	r.payoutClaimCalls = append(r.payoutClaimCalls, payoutClaimCall{id, providerName})
	if r.payoutClaimErr != nil {
		return PaymentWithdrawal{}, r.payoutClaimErr
	}
	if r.withdrawal.ID == "" {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	if r.withdrawal.Status != WithdrawalStatusApproved {
		return PaymentWithdrawal{}, ErrInvalidWithdrawalTransition
	}
	r.withdrawal.Status = WithdrawalStatusProcessing
	r.withdrawal.Provider = providerName
	return r.withdrawal, nil
}

type payoutClaimCall struct {
	id       string
	provider string
}

func (r *fakePaymentRepository) GetPaymentOrderByIDForApp(_ context.Context, _, _ string) (PaymentOrder, error) {
	if r.getOrderErr != nil {
		return PaymentOrder{}, r.getOrderErr
	}
	return r.getOrder, nil
}

func (r *fakePaymentRepository) GetPaymentOrderByAppReference(_ context.Context, _, _ string) (PaymentOrder, error) {
	if r.referenceErr != nil {
		return PaymentOrder{}, r.referenceErr
	}
	return r.referenceOrder, nil
}

func (r *fakePaymentRepository) CreatePaymentEvent(_ context.Context, input PaymentEventInput) (StoredPaymentEvent, error) {
	r.events = append(r.events, input)
	return StoredPaymentEvent{ID: "evt_test", Duplicate: r.duplicate}, nil
}

func (r *fakePaymentRepository) GetPaymentOrderByProviderOrderID(_ context.Context, _, _ string) (PaymentOrder, error) {
	r.orderLookups++
	if r.orderErr != nil {
		return PaymentOrder{}, r.orderErr
	}
	return r.order, nil
}

func (r *fakePaymentRepository) ListReconciliationCandidates(_ context.Context, _ time.Time, _ int) ([]PaymentOrder, error) {
	return r.reconcileCandidates, nil
}

func (r *fakePaymentRepository) ExpirePendingOrders(_ context.Context, _ int) ([]PaymentOrder, error) {
	if r.expireErr != nil {
		return nil, r.expireErr
	}
	expired := make([]PaymentOrder, len(r.expireCandidates))
	for i, order := range r.expireCandidates {
		order.Status = provider.StatusExpired
		expired[i] = order
	}
	return expired, nil
}

func (r *fakePaymentRepository) GetIdempotencyRecord(_ context.Context, appID, endpoint, key string) (IdempotencyRecord, bool, error) {
	record, ok := r.idempotencyRecords[idempotencyScope{appID, endpoint, key}]
	return record, ok, r.idempotencyErr
}

func (r *fakePaymentRepository) ClaimIdempotencyKey(_ context.Context, appID, endpoint, key, requestHash string, _ time.Duration) (IdempotencyRecord, bool, error) {
	if r.idempotencyErr != nil {
		return IdempotencyRecord{}, false, r.idempotencyErr
	}
	scope := idempotencyScope{appID, endpoint, key}
	if existing, ok := r.idempotencyRecords[scope]; ok {
		return existing, false, nil
	}
	if r.idempotencyRecords == nil {
		r.idempotencyRecords = map[idempotencyScope]IdempotencyRecord{}
	}
	record := IdempotencyRecord{Key: key, AppID: appID, Endpoint: endpoint, RequestHash: requestHash}
	r.idempotencyRecords[scope] = record
	return record, true, nil
}

func (r *fakePaymentRepository) StoreIdempotencyResponse(_ context.Context, appID, endpoint, key string, status int, body string) error {
	if r.idempotencyErr != nil {
		return r.idempotencyErr
	}
	scope := idempotencyScope{appID, endpoint, key}
	record := r.idempotencyRecords[scope]
	record.Completed = true
	record.ResponseStatus = status
	record.ResponseBody = body
	if r.idempotencyRecords == nil {
		r.idempotencyRecords = map[idempotencyScope]IdempotencyRecord{}
	}
	r.idempotencyRecords[scope] = record
	return nil
}

func (r *fakePaymentRepository) DeleteIdempotencyKey(_ context.Context, appID, endpoint, key string) error {
	delete(r.idempotencyRecords, idempotencyScope{appID, endpoint, key})
	return r.idempotencyErr
}

func (r *fakePaymentRepository) DeleteExpiredIdempotencyKeys(_ context.Context) (int64, error) {
	return r.expiredIdempotencyCount, r.idempotencyErr
}

type idempotencyScope struct {
	appID    string
	endpoint string
	key      string
}

func (r *fakePaymentRepository) SearchPaymentOrders(_ context.Context, filter PaymentOrderSearchFilter) (PaymentOrderSearchResult, error) {
	r.searchFilter = filter
	if r.searchResult.Limit == 0 {
		r.searchResult.Limit = filter.Limit
		r.searchResult.Offset = filter.Offset
	}
	return r.searchResult, nil
}

func (r *fakePaymentRepository) ListPaymentEvents(_ context.Context, filter PaymentEventListFilter) (PaymentEventListResult, error) {
	r.eventFilter = filter
	if r.eventResult.Limit == 0 {
		r.eventResult.Limit = filter.Limit
		r.eventResult.Offset = filter.Offset
	}
	return r.eventResult, nil
}

func (r *fakePaymentRepository) ListWebhookDeliveries(_ context.Context, filter PaymentWebhookDeliveryListFilter) (PaymentWebhookDeliveryListResult, error) {
	r.deliveryFilter = filter
	if r.deliveryResult.Limit == 0 {
		r.deliveryResult.Limit = filter.Limit
		r.deliveryResult.Offset = filter.Offset
	}
	return r.deliveryResult, nil
}

func (r *fakePaymentRepository) ReplayWebhookDelivery(_ context.Context, deliveryID string) error {
	r.replayedDeliveryID = deliveryID
	return nil
}

func (r *fakePaymentRepository) GetPaymentMetrics(_ context.Context, staleBefore time.Time) (PaymentMetrics, error) {
	r.metricsStaleBefore = staleBefore
	return r.metrics, nil
}

func (r *fakePaymentRepository) ApplyWebhookEvent(_ context.Context, input ApplyWebhookEventInput) (PaymentOrder, error) {
	r.applied = append(r.applied, input)
	if r.updatedOrder.ID != "" {
		return r.updatedOrder, nil
	}
	return PaymentOrder{ID: input.PaymentOrderID, Status: input.ToStatus, ProviderStatus: input.ProviderStatus}, nil
}

func (r *fakePaymentRepository) MarkEventProcessed(_ context.Context, _, _, processingError string) error {
	r.marked = append(r.marked, processingError)
	return nil
}

func (r *fakePaymentRepository) CreatePaymentWebhookEndpoint(_ context.Context, input CreatePaymentWebhookEndpointRepositoryInput) (PaymentWebhookEndpoint, error) {
	r.endpointInput = input
	if r.endpoint.ID != "" {
		return r.endpoint, nil
	}
	return PaymentWebhookEndpoint{
		ID:         input.ID,
		AppID:      input.AppID,
		URL:        input.URL,
		EventTypes: input.EventTypes,
		Status:     "active",
	}, nil
}

func (r *fakePaymentRepository) ListWebhookEndpoints(_ context.Context, _ string) ([]PaymentWebhookEndpoint, error) {
	return nil, nil
}

func (r *fakePaymentRepository) CreateWebhookDeliveriesForEvent(_ context.Context, eventID, eventType string) (int64, error) {
	r.createdDeliveries++
	r.deliveryForEventCalls = append(r.deliveryForEventCalls, deliveryForEventCall{eventID, eventType})
	return r.createdDeliveries, nil
}

type deliveryForEventCall struct {
	eventID   string
	eventType string
}

func (r *fakePaymentRepository) ReplayFailedWebhookDeliveriesForEvent(_ context.Context, eventID string) (int64, error) {
	r.replayedEventID = eventID
	if r.replayedEventCount == 0 {
		return 0, nil
	}
	return r.replayedEventCount, nil
}

func (r *fakePaymentRepository) ClaimDueWebhookDeliveries(_ context.Context, _ int) ([]PaymentWebhookDeliveryJob, error) {
	return r.deliveryJobs, nil
}

func (r *fakePaymentRepository) RecordWebhookDeliveryAttempt(ctx context.Context, input RecordWebhookDeliveryAttemptInput) error {
	r.recordedAttempts = append(r.recordedAttempts, input)
	r.recordCtxWasCancelled = append(r.recordCtxWasCancelled, ctx.Err() != nil)
	return nil
}

func (r *fakePaymentRepository) CreateProviderAccount(_ context.Context, providerKind, name, environment, baseURL, credentialsEncrypted string) (PaymentProviderAccount, error) {
	return PaymentProviderAccount{ID: "acct_test", Provider: providerKind, Name: name, Environment: environment, BaseURL: baseURL, CredentialsEncrypted: credentialsEncrypted}, nil
}

func (r *fakePaymentRepository) UpdateProviderAccount(_ context.Context, id, name, environment, baseURL, status string, credentialsEncrypted *string) (PaymentProviderAccount, error) {
	return PaymentProviderAccount{ID: id, Name: name, Environment: environment, BaseURL: baseURL, Status: status}, nil
}

func (r *fakePaymentRepository) DeleteProviderAccount(_ context.Context, _ string) error {
	return nil
}

func (r *fakePaymentRepository) ListProviderAccounts(_ context.Context) ([]PaymentProviderAccount, error) {
	return nil, nil
}

func (r *fakePaymentRepository) SetDefaultProviderAccount(_ context.Context, _ string) error {
	return nil
}

// GetDefaultProviderAccount defaults to a valid encrypted-credentials row for
// whatever kind is requested, so resolveProvider succeeds unless a test
// explicitly opts into failure via providerAccountErr.
func (r *fakePaymentRepository) GetDefaultProviderAccount(_ context.Context, kind string) (PaymentProviderAccount, error) {
	if r.providerAccountErr != nil {
		return PaymentProviderAccount{}, r.providerAccountErr
	}
	if r.providerAccount.ID != "" {
		return r.providerAccount, nil
	}
	return PaymentProviderAccount{ID: "acct_test", Provider: kind, Name: kind, Status: "active", CredentialsEncrypted: testEncryptedCredentials}, nil
}

func (r *fakePaymentRepository) GetPaymentAppByID(_ context.Context, appID string) (PaymentApp, error) {
	return PaymentApp{ID: appID, Name: "Test App", Status: "active", FeeType: "percentage", FeePercent: "0", FeeFixed: "0"}, nil
}

func (r *fakePaymentRepository) UpdateAppFees(_ context.Context, appID string, input UpdateAppFeesInput) (PaymentApp, error) {
	return PaymentApp{ID: appID, Name: "Test App", Status: "active", FeeType: input.FeeType, FeePercent: input.FeePercent, FeeFixed: input.FeeFixed}, nil
}

func (r *fakePaymentRepository) DeletePaymentApp(_ context.Context, appID string) (PaymentApp, error) {
	if r.deletePaymentAppErr != nil {
		return PaymentApp{}, r.deletePaymentAppErr
	}
	return PaymentApp{ID: appID, Name: "Test App", Status: "deleted"}, nil
}

func (r *fakePaymentRepository) GetAppBalance(_ context.Context, appID string) (AppBalance, error) {
	return AppBalance{AppID: appID, Currency: "TZS", AvailableBalance: "0.00", TotalRevenue: "0.00", TotalPlatformFees: "0.00", TotalWithdrawn: "0.00", PendingOrderTotal: "0.00"}, nil
}

func (r *fakePaymentRepository) ListLedgerEntries(_ context.Context, filter LedgerEntryListFilter) (LedgerEntryListResult, error) {
	return LedgerEntryListResult{Items: []LedgerEntry{}, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *fakePaymentRepository) CreateWithdrawal(_ context.Context, input CreateWithdrawalInput) (PaymentWithdrawal, error) {
	return PaymentWithdrawal{ID: "wd_test", AppID: input.AppID, Amount: input.Amount, Currency: input.Currency, DestinationType: input.DestinationType, Status: WithdrawalStatusRequested, RequestedBy: input.RequestedBy}, nil
}

func (r *fakePaymentRepository) ListWithdrawals(_ context.Context, filter WithdrawalListFilter) (WithdrawalListResult, error) {
	return WithdrawalListResult{Items: []PaymentWithdrawal{}, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *fakePaymentRepository) GetWithdrawalByID(_ context.Context, id string) (PaymentWithdrawal, error) {
	if r.withdrawalErr != nil {
		return PaymentWithdrawal{}, r.withdrawalErr
	}
	if r.withdrawal.ID == "" {
		return PaymentWithdrawal{}, ErrWithdrawalNotFound
	}
	return r.withdrawal, nil
}

func (r *fakePaymentRepository) DispatchWithdrawal(_ context.Context, id, providerName, providerPayoutID, status, providerStatus string) (PaymentWithdrawal, error) {
	r.dispatchCalls = append(r.dispatchCalls, dispatchWithdrawalCall{id, providerName, providerPayoutID, status, providerStatus})
	if r.dispatchErr != nil {
		return PaymentWithdrawal{}, r.dispatchErr
	}
	r.withdrawal = PaymentWithdrawal{ID: id, Provider: providerName, ProviderPayoutID: providerPayoutID, Status: status, ProviderStatus: providerStatus}
	return r.withdrawal, nil
}

func (r *fakePaymentRepository) RecordPayoutFailure(_ context.Context, id, providerName, reason string) (PaymentWithdrawal, error) {
	r.recordPayoutFailureCalls = append(r.recordPayoutFailureCalls, recordPayoutFailureCall{id, providerName, reason})
	if r.recordPayoutFailureErr != nil {
		return PaymentWithdrawal{}, r.recordPayoutFailureErr
	}
	r.withdrawal = PaymentWithdrawal{ID: id, Provider: providerName, Status: WithdrawalStatusApproved, FailureReason: reason}
	return r.withdrawal, nil
}

func (r *fakePaymentRepository) GetWithdrawalByProviderPayoutID(_ context.Context, _, _ string) (PaymentWithdrawal, error) {
	if r.payoutWithdrawalErr != nil {
		return PaymentWithdrawal{}, r.payoutWithdrawalErr
	}
	return r.payoutWithdrawal, nil
}

func (r *fakePaymentRepository) ListPayoutReconciliationCandidates(_ context.Context, _ time.Time, _ int) ([]PaymentWithdrawal, error) {
	return r.payoutReconciliationCandidates, nil
}

func (r *fakePaymentRepository) ApproveWithdrawal(_ context.Context, id, approvedBy string) (PaymentWithdrawal, error) {
	w := PaymentWithdrawal{ID: id, Amount: "1000.00", Currency: "TZS", DestinationType: "mobile_money", Status: WithdrawalStatusApproved, ApprovedBy: approvedBy}
	r.withdrawal = w
	return w, nil
}

func (r *fakePaymentRepository) RejectWithdrawal(_ context.Context, id string) (PaymentWithdrawal, error) {
	return PaymentWithdrawal{ID: id, Status: WithdrawalStatusRejected}, nil
}

func (r *fakePaymentRepository) MarkWithdrawalPaid(_ context.Context, id string) (PaymentWithdrawal, error) {
	return PaymentWithdrawal{ID: id, Status: WithdrawalStatusPaid}, nil
}

func (r *fakePaymentRepository) MarkWithdrawalFailed(_ context.Context, id, notes string) (PaymentWithdrawal, error) {
	return PaymentWithdrawal{ID: id, Status: WithdrawalStatusFailed, Notes: notes}, nil
}

func (r *fakePaymentRepository) FindUserIDByEmail(_ context.Context, email string) (string, error) {
	if email == "" {
		return "", ErrUserNotFound
	}
	return "user_test", nil
}

func (r *fakePaymentRepository) AddAppMember(_ context.Context, appID, userID, addedBy string) (AppMember, error) {
	return AppMember{ID: "member_test", AppID: appID, UserID: userID, AddedBy: addedBy}, nil
}

func (r *fakePaymentRepository) ListAppMembers(_ context.Context, appID string) ([]AppMember, error) {
	if r.appMembers != nil {
		return r.appMembers, nil
	}
	return []AppMember{}, nil
}

func (r *fakePaymentRepository) RemoveAppMember(_ context.Context, _, _ string) error {
	return nil
}

func (r *fakePaymentRepository) IsAppMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

func (r *fakePaymentRepository) ListAppsForUser(_ context.Context, _ string) ([]PaymentApp, error) {
	return []PaymentApp{}, nil
}

func (r *fakePaymentRepository) GetPaymentOrderByID(_ context.Context, orderID string) (PaymentOrder, error) {
	if r.getOrderErr != nil {
		return PaymentOrder{}, r.getOrderErr
	}
	if r.getOrder.ID != "" {
		return r.getOrder, nil
	}
	return PaymentOrder{ID: orderID}, nil
}

func (r *fakePaymentRepository) GetPaymentEventByID(_ context.Context, eventID string) (PaymentEvent, error) {
	if r.getEventErr != nil {
		return PaymentEvent{}, r.getEventErr
	}
	if r.getEvent.ID != "" {
		return r.getEvent, nil
	}
	return PaymentEvent{ID: eventID, EventType: EventTypePaymentUpdated}, nil
}

func (r *fakePaymentRepository) ClaimRefund(_ context.Context, input ClaimRefundInput) (PaymentOrder, PaymentRefund, error) {
	r.claimRefundInputs = append(r.claimRefundInputs, input)
	if r.claimRefundErr != nil {
		return PaymentOrder{}, PaymentRefund{}, r.claimRefundErr
	}
	order := PaymentOrder{ID: input.OrderID, AppID: "app_test", Provider: "sonicpesa", Amount: "10000.00", Currency: "TZS", Status: provider.StatusPaid}
	refund := PaymentRefund{ID: "refund_test", PaymentOrderID: input.OrderID, AppID: "app_test", Amount: "10000.00", Currency: "TZS", Status: RefundStatusProcessing}
	if input.Amount != "" {
		refund.Amount = input.Amount
	}
	if input.Currency != "" {
		refund.Currency = input.Currency
	}
	if input.ProviderRefundID != "" {
		refund.ProviderRefundID = input.ProviderRefundID
	}
	r.claimedRefund = refund
	return order, refund, nil
}

func (r *fakePaymentRepository) AttachRefundProviderID(_ context.Context, refundID, providerRefundID string) error {
	if r.attachedRefundIDs == nil {
		r.attachedRefundIDs = map[string]string{}
	}
	r.attachedRefundIDs[refundID] = providerRefundID
	return r.attachRefundErr
}

func (r *fakePaymentRepository) ConfirmRefund(_ context.Context, refundID, providerRefundID, providerStatus string) (PaymentOrder, PaymentRefund, error) {
	r.confirmRefundCalls = append(r.confirmRefundCalls, confirmRefundCall{refundID, providerRefundID, providerStatus})
	if r.confirmRefundErr != nil {
		return PaymentOrder{}, PaymentRefund{}, r.confirmRefundErr
	}
	refund := r.claimedRefund
	refund.Status = RefundStatusConfirmed
	if providerRefundID != "" {
		refund.ProviderRefundID = providerRefundID
	}
	return PaymentOrder{ID: refund.PaymentOrderID, Status: provider.StatusReversed}, refund, nil
}

func (r *fakePaymentRepository) FailRefund(_ context.Context, refundID, reason string) (PaymentRefund, error) {
	r.failRefundCalls = append(r.failRefundCalls, failRefundCall{refundID, reason})
	if r.failRefundErr != nil {
		return PaymentRefund{}, r.failRefundErr
	}
	refund := r.claimedRefund
	refund.Status = RefundStatusFailed
	return refund, nil
}

func (r *fakePaymentRepository) RefundedTotal(_ context.Context, _ string) (string, error) {
	return "0.00", nil
}

type confirmRefundCall struct {
	refundID         string
	providerRefundID string
	providerStatus   string
}

type failRefundCall struct {
	refundID string
	reason   string
}

type smsSendCall struct {
	appID   string
	phone   string
	message string
}

type fakeSMSSender struct {
	calls []smsSendCall
	err   error
}

func (f *fakeSMSSender) SendSMS(_ context.Context, appID, phone, message string) error {
	f.calls = append(f.calls, smsSendCall{appID, phone, message})
	return f.err
}

func testServiceOptions() ServiceOptions {
	return ServiceOptions{
		DeliverySigningSecret:          "test-delivery-secret",
		DeliveryMaxAttempts:            3,
		DeliveryTimeout:                time.Second,
		ReconciliationStaleAfter:       time.Minute,
		ReconciliationBatchSize:        10,
		PayoutReconciliationStaleAfter: time.Minute,
	}
}

func TestHandleProviderWebhookStoresDuplicateWithoutReprocessing(t *testing.T) {
	repo := &fakePaymentRepository{duplicate: true}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusPaid,
		},
	}), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Duplicate {
		t.Fatal("expected duplicate result")
	}
	if repo.orderLookups != 0 {
		t.Fatalf("expected no order lookups for duplicate event, got %d", repo.orderLookups)
	}
	if len(repo.applied) != 0 {
		t.Fatalf("expected no applied updates for duplicate event, got %d", len(repo.applied))
	}
}

func TestHandleProviderWebhookMarksUnmatchedOrderProcessed(t *testing.T) {
	repo := &fakePaymentRepository{orderErr: ErrPaymentOrderNotFound}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_missing",
			NormalizedStatus: provider.StatusPaid,
		},
	}), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_missing"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Processed {
		t.Fatal("expected unmatched event to be marked processed")
	}
	if result.OrderFound {
		t.Fatal("expected order_found false")
	}
	if len(repo.marked) != 1 {
		t.Fatalf("expected one processed mark, got %d", len(repo.marked))
	}
}

func TestHandleProviderWebhookVerificationFailureIsStored(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name:      "sonicpesa",
		verifyErr: errors.New("bad signature"),
	}), testCipher, testServiceOptions(), nil)

	_, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123"}`))
	if !errors.Is(err, ErrWebhookVerificationFailed) {
		t.Fatalf("expected verification error, got %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected invalid webhook event to be stored, got %d", len(repo.events))
	}
	if repo.events[0].SignatureValid {
		t.Fatal("expected signature_valid false")
	}
	if repo.events[0].EventType != "webhook.signature_invalid" {
		t.Fatalf("expected signature invalid event type, got %q", repo.events[0].EventType)
	}
}

func TestTransitionStatusDoesNotDowngradeFinalState(t *testing.T) {
	if got := transitionStatus(provider.StatusPaid, provider.StatusPending); got != provider.StatusPaid {
		t.Fatalf("expected paid to stay paid when pending arrives, got %q", got)
	}
	if got := transitionStatus(provider.StatusPaid, provider.StatusReversed); got != provider.StatusReversed {
		t.Fatalf("expected paid to become reversed, got %q", got)
	}
	if got := transitionStatus(provider.StatusPending, provider.StatusPaid); got != provider.StatusPaid {
		t.Fatalf("expected pending to become paid, got %q", got)
	}
}

func TestCreateOrderCallsProviderAndPersistsMapping(t *testing.T) {
	repo := &fakePaymentRepository{referenceErr: ErrPaymentOrderNotFound}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		createResp: provider.ProviderOrder{
			OrderID:        "sp_123",
			Reference:      "S123",
			Status:         provider.StatusPending,
			ProviderStatus: "PENDING",
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	order, errs, err := service.CreateOrder(context.Background(), PaymentApp{ID: "app_test"}, CreatePaymentOrderInput{
		Provider:          "sonicpesa",
		Amount:            "10000",
		Currency:          "TZS",
		BuyerName:         "Customer",
		BuyerEmail:        "customer@example.com",
		BuyerPhone:        "255700000000",
		ExternalReference: "app-order-1",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if errs.Any() {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
	if p.createCalled != 1 {
		t.Fatalf("expected provider create called once, got %d", p.createCalled)
	}
	// Pending local row first: no provider order id yet, so a concurrent
	// create with the same reference serializes instead of double-creating
	// at the provider.
	if repo.createOrderInput.ProviderOrderID != "" {
		t.Fatalf("expected pending insert without provider order id, got %q", repo.createOrderInput.ProviderOrderID)
	}
	if repo.createOrderInput.Status != provider.StatusPending {
		t.Fatalf("expected pending insert status, got %q", repo.createOrderInput.Status)
	}
	if repo.createOrderInput.Amount != "10000.00" {
		t.Fatalf("expected normalized amount 10000.00, got %q", repo.createOrderInput.Amount)
	}
	if p.createReq.ExternalReference != "app-order-1" {
		t.Fatalf("expected external reference forwarded to provider, got %q", p.createReq.ExternalReference)
	}
	if len(repo.updateProviderCalls) != 1 || repo.updateProviderCalls[0].providerOrderID != "sp_123" {
		t.Fatalf("expected provider details adopted after provider call, got %+v", repo.updateProviderCalls)
	}
	if order.ProviderOrderID != "sp_123" {
		t.Fatalf("expected returned order provider id sp_123, got %q", order.ProviderOrderID)
	}
}

func TestCreateOrderReturnsExistingForExternalReference(t *testing.T) {
	repo := &fakePaymentRepository{
		referenceOrder: PaymentOrder{ID: "pay_existing", ExternalReference: "app-order-1"},
	}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	order, errs, err := service.CreateOrder(context.Background(), PaymentApp{ID: "app_test"}, CreatePaymentOrderInput{
		Provider:          "sonicpesa",
		Amount:            "10000",
		Currency:          "TZS",
		BuyerName:         "Customer",
		BuyerEmail:        "customer@example.com",
		BuyerPhone:        "255700000000",
		ExternalReference: "app-order-1",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if errs.Any() {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
	if order.ID != "pay_existing" {
		t.Fatalf("expected existing order, got %q", order.ID)
	}
	if p.createCalled != 0 {
		t.Fatalf("expected provider create not called for existing reference, got %d", p.createCalled)
	}
}

func TestListAppsCapsPagination(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	result, err := service.ListApps(context.Background(), 500, -1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Limit != 200 || result.Offset != 0 {
		t.Fatalf("expected capped pagination, got limit=%d offset=%d", result.Limit, result.Offset)
	}
}

func TestRefreshOrderUsesProviderStatusWithoutDowngrade(t *testing.T) {
	repo := &fakePaymentRepository{
		getOrder: PaymentOrder{
			ID:              "pay_test",
			AppID:           "app_test",
			Provider:        "sonicpesa",
			ProviderOrderID: "sp_123",
			Status:          provider.StatusPaid,
		},
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		statusResp: provider.ProviderStatus{
			OrderID:        "sp_123",
			Status:         provider.StatusPending,
			ProviderStatus: "PENDING",
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	order, err := service.RefreshOrder(context.Background(), PaymentApp{ID: "app_test"}, "pay_test")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.statusCalled != 1 {
		t.Fatalf("expected provider status called once, got %d", p.statusCalled)
	}
	if len(repo.applied) != 1 || repo.applied[0].ToStatus != provider.StatusPaid {
		t.Fatalf("expected paid not to downgrade, got %+v", repo.applied)
	}
	if order.Status != provider.StatusPaid {
		t.Fatalf("expected returned status paid, got %q", order.Status)
	}
}

func TestReconcilePaymentsUpdatesChangedStatusAndCreatesDeliveries(t *testing.T) {
	repo := &fakePaymentRepository{
		reconcileCandidates: []PaymentOrder{
			{
				ID:              "pay_test",
				AppID:           "app_test",
				Provider:        "sonicpesa",
				ProviderOrderID: "sp_123",
				Status:          provider.StatusPending,
				Amount:          "10000.00",
				Currency:        "TZS",
			},
		},
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		statusResp: provider.ProviderStatus{
			OrderID:        "sp_123",
			TransactionID:  "TXN123",
			Status:         provider.StatusPaid,
			ProviderStatus: "SUCCESS",
			Raw: map[string]any{
				"order_id":       "sp_123",
				"payment_status": "SUCCESS",
				"transid":        "TXN123",
			},
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.ReconcilePayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Scanned != 1 || result.Checked != 1 || result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("unexpected reconciliation result: %+v", result)
	}
	if p.statusCalled != 1 {
		t.Fatalf("expected provider status called once, got %d", p.statusCalled)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected one reconciliation event, got %d", len(repo.events))
	}
	if repo.events[0].NormalizedStatus != provider.StatusPaid {
		t.Fatalf("expected paid event status, got %q", repo.events[0].NormalizedStatus)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("expected one status update applied, got %d", len(repo.applied))
	}
	applied := repo.applied[0]
	if applied.Source != "reconciliation" || applied.EventID != "evt_test" {
		t.Fatalf("expected reconciliation status update linked to event, got %+v", applied)
	}
	if repo.createdDeliveries != 1 || result.DeliveriesCreated != 1 {
		t.Fatalf("expected one delivery created, repo=%d result=%d", repo.createdDeliveries, result.DeliveriesCreated)
	}
	// Regression test: an order marked "paid" via reconciliation (not a
	// live webhook) must still credit the app's ledger — this used to
	// silently never happen, which is why apps showed 0.00 balance despite
	// having real paid orders.
	if !applied.PostLedger {
		t.Fatalf("expected reconciliation to post ledger entries on transition to paid, got %+v", applied)
	}
	if applied.AppID != "app_test" || applied.GrossAmount != "10000.00" || applied.Currency != "TZS" {
		t.Fatalf("expected ledger fields from the order, got %+v", applied)
	}
}

func TestAttemptPayoutDispatchesAndMarksPaidOnSyncSuccess(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "mobile_money", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		disburseResp: provider.DisburseResult{
			ProviderPayoutID: "payout_123",
			Status:           provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	withdrawal, err := service.AttemptPayout(context.Background(), "wd_test")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected Disburse called once, got %d", p.disburseCalled)
	}
	if len(repo.dispatchCalls) != 1 || repo.dispatchCalls[0].providerPayoutID != "payout_123" || repo.dispatchCalls[0].status != WithdrawalStatusProcessing {
		t.Fatalf("expected withdrawal dispatched into processing with the provider payout id, got %+v", repo.dispatchCalls)
	}
	if withdrawal.Status != WithdrawalStatusPaid {
		t.Fatalf("expected synchronous paid result to mark the withdrawal paid, got %q", withdrawal.Status)
	}
}

// TestAttemptPayoutUsesConfiguredPayoutProviderNotAHardcodedLiteral guards
// against regressing to a hardcoded "sonicpesa" literal in AttemptPayout —
// the provider it dispatches through must come from ServiceOptions.PayoutProvider,
// so a future second payout-capable provider is a config change, not a code change.
func TestAttemptPayoutUsesConfiguredPayoutProviderNotAHardcodedLiteral(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "mobile_money", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{
		name: "otherpay",
		disburseResp: provider.DisburseResult{
			ProviderPayoutID: "payout_999",
			Status:           provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}
	opts := testServiceOptions()
	opts.PayoutProvider = "otherpay"
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	if _, err := service.AttemptPayout(context.Background(), "wd_test"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected the configured provider's Disburse to be called once, got %d", p.disburseCalled)
	}
	if len(repo.dispatchCalls) != 1 || repo.dispatchCalls[0].provider != "otherpay" {
		t.Fatalf("expected dispatch to record the configured provider name, got %+v", repo.dispatchCalls)
	}
}

func TestAttemptPayoutMarksFailedAndReversesLedgerOnSyncFailure(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "bank", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		disburseResp: provider.DisburseResult{
			ProviderPayoutID: "payout_456",
			Status:           provider.StatusFailed,
			ProviderStatus:   "REJECTED",
		},
	}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	withdrawal, err := service.AttemptPayout(context.Background(), "wd_test")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// MarkWithdrawalFailed (reused as-is from the manual fallback) is what
	// reverses the ledger debit posted at approval time — dispatching
	// straight into 'failed' without going through it would silently skip
	// that reversal, which is the bug this test guards against.
	if withdrawal.Status != WithdrawalStatusFailed {
		t.Fatalf("expected synchronous failed result to mark the withdrawal failed (reversing the debit), got %q", withdrawal.Status)
	}
	if len(repo.dispatchCalls) != 1 || repo.dispatchCalls[0].status != WithdrawalStatusProcessing {
		t.Fatalf("expected dispatch to always land in processing before a terminal transition, got %+v", repo.dispatchCalls)
	}
}

func TestAttemptPayoutRecordsFailureReasonOnDisburseError(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "bank", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{name: "sonicpesa", disburseErr: errors.New("sonicpesa unreachable")}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	withdrawal, err := service.AttemptPayout(context.Background(), "wd_test")
	if err == nil {
		t.Fatal("expected the disburse error to be returned")
	}
	if len(repo.dispatchCalls) != 0 {
		t.Fatalf("expected no dispatch when Disburse itself failed, got %+v", repo.dispatchCalls)
	}
	if len(repo.recordPayoutFailureCalls) != 1 || repo.recordPayoutFailureCalls[0].reason != "sonicpesa unreachable" {
		t.Fatalf("expected the failure reason to be recorded, got %+v", repo.recordPayoutFailureCalls)
	}
	// Status must stay 'approved' — a Disburse error never auto-fails the
	// withdrawal, since the money may already be in flight at the provider.
	if withdrawal.Status != WithdrawalStatusApproved {
		t.Fatalf("expected withdrawal to remain approved after a disburse error, got %q", withdrawal.Status)
	}
}

func TestAttemptPayoutIsNotRetriedOnceDispatched(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "bank", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{
		name:         "sonicpesa",
		disburseResp: provider.DisburseResult{ProviderPayoutID: "payout_789", Status: provider.StatusProcessing, ProviderStatus: "PENDING"},
	}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	if _, err := service.AttemptPayout(context.Background(), "wd_test"); err != nil {
		t.Fatalf("expected first attempt to succeed, got %v", err)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected exactly one Disburse call after the first attempt, got %d", p.disburseCalled)
	}

	// The withdrawal is now 'processing' in the fake (mirroring the real
	// claim-first guard), so a second attempt must not dispatch again.
	if _, err := service.AttemptPayout(context.Background(), "wd_test"); !errors.Is(err, ErrInvalidWithdrawalTransition) {
		t.Fatalf("expected retrying an already-dispatched withdrawal to fail with ErrInvalidWithdrawalTransition, got %v", err)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected no second Disburse call, got %d", p.disburseCalled)
	}
}

func TestApproveWithdrawalDispatchesPayoutWhenAutomatedPayoutsEnabled(t *testing.T) {
	repo := &fakePaymentRepository{}
	p := &fakePaymentProvider{
		name:         "sonicpesa",
		disburseResp: provider.DisburseResult{ProviderPayoutID: "payout_abc", Status: provider.StatusPaid, ProviderStatus: "SUCCESS"},
	}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	withdrawal, err := service.ApproveWithdrawal(context.Background(), "wd_test", "admin_1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected approval to trigger an automatic payout attempt, got %d calls", p.disburseCalled)
	}
	if withdrawal.Status != WithdrawalStatusPaid {
		t.Fatalf("expected the dispatched result to be returned from ApproveWithdrawal, got %q", withdrawal.Status)
	}
}

func TestApproveWithdrawalDoesNotDispatchWhenAutomatedPayoutsDisabled(t *testing.T) {
	repo := &fakePaymentRepository{}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	withdrawal, err := service.ApproveWithdrawal(context.Background(), "wd_test", "admin_1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.disburseCalled != 0 {
		t.Fatalf("expected no payout attempt with automation disabled, got %d calls", p.disburseCalled)
	}
	if withdrawal.Status != WithdrawalStatusApproved {
		t.Fatalf("expected withdrawal to remain approved, got %q", withdrawal.Status)
	}
}

func TestReconcilePayoutsAppliesTerminalStatus(t *testing.T) {
	repo := &fakePaymentRepository{
		payoutReconciliationCandidates: []PaymentWithdrawal{
			{ID: "wd_test", Provider: "sonicpesa", ProviderPayoutID: "payout_123", Status: WithdrawalStatusProcessing, Amount: "5000.00", Currency: "TZS"},
		},
	}
	p := &fakePaymentProvider{
		name:             "sonicpesa",
		payoutStatusResp: provider.DisburseResult{ProviderPayoutID: "payout_123", Status: provider.StatusPaid, ProviderStatus: "SUCCESS"},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.ReconcilePayouts(context.Background(), 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Scanned != 1 || result.Checked != 1 || result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("unexpected reconciliation result: %+v", result)
	}
	if p.payoutStatusCalled != 1 {
		t.Fatalf("expected CheckPayoutStatus called once, got %d", p.payoutStatusCalled)
	}
}

func TestSearchPaymentOrdersNormalizesAdminFilters(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	_, err := service.SearchPaymentOrders(context.Background(), PaymentOrderSearchFilter{
		Query:    " app-order ",
		Provider: " SonicPesa ",
		Phone:    " 255700000000 ",
		Limit:    500,
		Offset:   -10,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if repo.searchFilter.Query != "app-order" {
		t.Fatalf("expected trimmed query, got %q", repo.searchFilter.Query)
	}
	if repo.searchFilter.Provider != "sonicpesa" {
		t.Fatalf("expected normalized provider, got %q", repo.searchFilter.Provider)
	}
	if repo.searchFilter.Phone != "255700000000" {
		t.Fatalf("expected trimmed phone, got %q", repo.searchFilter.Phone)
	}
	if repo.searchFilter.Limit != 200 || repo.searchFilter.Offset != 0 {
		t.Fatalf("expected capped pagination, got limit=%d offset=%d", repo.searchFilter.Limit, repo.searchFilter.Offset)
	}
}

func TestReplayFailedDeliveryQueuesRetry(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	if err := service.ReplayFailedDelivery(context.Background(), " del_test "); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if repo.replayedDeliveryID != "del_test" {
		t.Fatalf("expected trimmed delivery id, got %q", repo.replayedDeliveryID)
	}
}

func TestReplayEventCreatesMissingAndReplaysFailedDeliveries(t *testing.T) {
	repo := &fakePaymentRepository{replayedEventCount: 2}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	result, err := service.ReplayEvent(context.Background(), " evt_test ")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.CreatedDeliveries != 1 || result.ReplayedDeliveries != 2 || result.QueuedDeliveries != 3 {
		t.Fatalf("unexpected replay result: %+v", result)
	}
	if repo.replayedEventID != "evt_test" {
		t.Fatalf("expected trimmed replay event id, got %q", repo.replayedEventID)
	}
}

func TestPaymentMetricsUsesReconciliationWindow(t *testing.T) {
	repo := &fakePaymentRepository{metrics: PaymentMetrics{TotalEvents: 3}}
	opts := testServiceOptions()
	opts.ReconciliationStaleAfter = time.Hour
	service := NewService(repo, nil, testCipher, opts, nil)

	metrics, err := service.PaymentMetrics(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if metrics.TotalEvents != 3 {
		t.Fatalf("expected metrics response, got %+v", metrics)
	}
	if time.Since(repo.metricsStaleBefore) < time.Hour || time.Since(repo.metricsStaleBefore) > time.Hour+time.Minute {
		t.Fatalf("expected metrics stale cutoff about one hour ago, got %s", repo.metricsStaleBefore)
	}
}

func TestHandleProviderWebhookCreatesDeliveryJobs(t *testing.T) {
	repo := &fakePaymentRepository{
		order: PaymentOrder{
			ID:              "pay_test",
			AppID:           "app_test",
			Provider:        "sonicpesa",
			ProviderOrderID: "sp_123",
			Status:          provider.StatusPending,
		},
	}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123","status":"SUCCESS"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Processed || !result.OrderFound {
		t.Fatalf("expected processed matching result, got %+v", result)
	}
	if repo.createdDeliveries != 1 {
		t.Fatalf("expected one delivery batch created, got %d", repo.createdDeliveries)
	}
}

func TestHandleProviderWebhookSendsPaymentSuccessSMSToMembersWithPhone(t *testing.T) {
	repo := &fakePaymentRepository{
		order: PaymentOrder{
			ID:              "pay_test",
			AppID:           "app_test",
			Provider:        "sonicpesa",
			ProviderOrderID: "sp_123",
			Status:          provider.StatusPending,
			Amount:          "10000.00",
			Currency:        "TZS",
			BuyerName:       "Jabir Rwanjoro",
		},
		appMembers: []AppMember{
			{ID: "m1", AppID: "app_test", UserID: "u1", Phone: "255700000001"},
			{ID: "m2", AppID: "app_test", UserID: "u2", Phone: ""},
		},
	}
	sender := &fakeSMSSender{}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}), testCipher, testServiceOptions(), nil)
	service.SetSMSSender(sender)

	if _, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123","status":"SUCCESS"}`)); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("expected exactly one SMS sent (only the member with a phone), got %+v", sender.calls)
	}
	call := sender.calls[0]
	if call.appID != "app_test" || call.phone != "255700000001" {
		t.Fatalf("expected SMS sent to the phone-having member, got %+v", call)
	}
	if !strings.Contains(call.message, "10000.00") || !strings.Contains(call.message, "Jabir Rwanjoro") || !strings.Contains(call.message, "Test App") {
		t.Fatalf("expected message to include amount, payer, and app name, got %q", call.message)
	}
}

func TestHandleProviderWebhookSkipsSMSWhenNoneAreConfigured(t *testing.T) {
	repo := &fakePaymentRepository{
		order: PaymentOrder{
			ID:              "pay_test",
			AppID:           "app_test",
			Provider:        "sonicpesa",
			ProviderOrderID: "sp_123",
			Status:          provider.StatusPending,
			Amount:          "10000.00",
			Currency:        "TZS",
		},
	}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}), testCipher, testServiceOptions(), nil)
	// SetSMSSender deliberately never called — must not panic or fail the webhook.

	if _, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123","status":"SUCCESS"}`)); err != nil {
		t.Fatalf("expected nil error even with no SMS sender wired, got %v", err)
	}
}

func TestHandleProviderWebhookDoesNotSendSMSOnDuplicateEvent(t *testing.T) {
	repo := &fakePaymentRepository{
		duplicate: true,
		order: PaymentOrder{
			ID:              "pay_test",
			AppID:           "app_test",
			Provider:        "sonicpesa",
			ProviderOrderID: "sp_123",
			Status:          provider.StatusPending,
		},
		appMembers: []AppMember{{ID: "m1", AppID: "app_test", UserID: "u1", Phone: "255700000001"}},
	}
	sender := &fakeSMSSender{}
	service := NewService(repo, registryFor(&fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusPaid,
			ProviderStatus:   "SUCCESS",
		},
	}), testCipher, testServiceOptions(), nil)
	service.SetSMSSender(sender)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_123","status":"SUCCESS"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Duplicate {
		t.Fatalf("expected duplicate result, got %+v", result)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no SMS sent for a duplicate/replayed webhook, got %+v", sender.calls)
	}
}

func TestDeletePaymentAppSoftDeletes(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	app, err := service.DeletePaymentApp(context.Background(), "admin_1", "admin@example.com", "app_test")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if app.Status != "deleted" {
		t.Fatalf("expected status 'deleted', got %q", app.Status)
	}
}

func TestDeletePaymentAppPropagatesRepoError(t *testing.T) {
	repo := &fakePaymentRepository{deletePaymentAppErr: ErrPaymentAppNotFound}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	if _, err := service.DeletePaymentApp(context.Background(), "admin_1", "admin@example.com", "app_missing"); !errors.Is(err, ErrPaymentAppNotFound) {
		t.Fatalf("expected ErrPaymentAppNotFound, got %v", err)
	}
}

func TestCreateWebhookEndpointReturnsDerivedSecret(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	result, errs, err := service.CreateWebhookEndpoint(context.Background(), CreatePaymentWebhookEndpointInput{
		AppID: "app_test",
		URL:   "https://example.com/payments/webhook",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if errs.Any() {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
	if result.SigningSecret == "" {
		t.Fatal("expected signing secret")
	}
	if repo.endpointInput.SecretHash != hashAPIKey(result.SigningSecret) {
		t.Fatal("expected stored secret hash to match returned signing secret")
	}
	wantDefaults := map[string]bool{EventTypePaymentUpdated: true, EventTypePaymentRefunded: true, EventTypePaymentExpired: true}
	if len(result.Endpoint.EventTypes) != len(wantDefaults) {
		t.Fatalf("expected default event types %v, got %v", wantDefaults, result.Endpoint.EventTypes)
	}
	for _, eventType := range result.Endpoint.EventTypes {
		if !wantDefaults[eventType] {
			t.Fatalf("unexpected default event type %q in %v", eventType, result.Endpoint.EventTypes)
		}
	}
}

func TestProcessDueDeliveriesSignsAndPostsWebhook(t *testing.T) {
	var receivedSignature string
	var receivedTimestamp string
	var receivedEventID string
	var receivedPayload WebhookDeliveryPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-AZsubay-Signature")
		receivedTimestamp = r.Header.Get("X-AZsubay-Timestamp")
		receivedEventID = r.Header.Get("X-AZsubay-Event-ID")
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("decode delivery payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &fakePaymentRepository{
		deliveryJobs: []PaymentWebhookDeliveryJob{
			{
				ID:           "del_test",
				EventID:      "evt_test",
				EndpointID:   "endpoint_test",
				EndpointURL:  server.URL,
				AppID:        "app_test",
				AttemptCount: 1,
				EventType:    "payment.updated",
				Provider:     "sonicpesa",
				ReceivedAt:   time.Now().UTC(),
				PaymentOrder: PaymentOrder{
					ID:              "pay_test",
					Provider:        "sonicpesa",
					ProviderOrderID: "sp_123",
					Status:          provider.StatusPaid,
					Amount:          "10000.00",
					Currency:        "TZS",
					BuyerPhone:      "255700000000",
				},
			},
		},
	}
	client := server.Client()
	opts := testServiceOptions()
	opts.HTTPClient = client
	service := NewService(repo, nil, testCipher, opts, nil)

	result, err := service.ProcessDueDeliveries(context.Background(), 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Delivered != 1 {
		t.Fatalf("expected one delivered webhook, got %+v", result)
	}
	if len(repo.recordedAttempts) != 1 || !repo.recordedAttempts[0].Success {
		t.Fatalf("expected successful delivery attempt, got %+v", repo.recordedAttempts)
	}
	if receivedEventID != "evt_test" {
		t.Fatalf("expected event id header evt_test, got %q", receivedEventID)
	}
	if receivedSignature == "" || receivedTimestamp == "" {
		t.Fatal("expected signature and timestamp headers")
	}
	if receivedPayload.PaymentID != "pay_test" || receivedPayload.Status != provider.StatusPaid {
		t.Fatalf("unexpected delivery payload: %+v", receivedPayload)
	}
}

func TestProcessDueDeliveriesRecordsWithCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &fakePaymentRepository{
		deliveryJobs: []PaymentWebhookDeliveryJob{
			{
				ID:           "del_cancelled",
				EventID:      "evt_cancelled",
				EndpointID:   "endpoint_cancelled",
				EndpointURL:  server.URL,
				AppID:        "app_test",
				AttemptCount: 1,
				EventType:    "payment.updated",
				Provider:     "sonicpesa",
				ReceivedAt:   time.Now().UTC(),
				PaymentOrder: PaymentOrder{
					ID:              "pay_cancelled",
					Provider:        "sonicpesa",
					ProviderOrderID: "sp_cancelled",
					Status:          provider.StatusPaid,
					Amount:          "5000.00",
					Currency:        "TZS",
				},
			},
		},
	}
	client := server.Client()
	opts := testServiceOptions()
	opts.HTTPClient = client
	service := NewService(repo, nil, testCipher, opts, nil)

	// Cancel the parent context before processing — as happens when the
	// inline 25s attempt times out or the process shuts down mid-flight.
	// The delivery attempt itself may fail, but recording its result must
	// still be attempted with a live context so the row never gets stuck
	// in 'processing'.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.ProcessDueDeliveries(ctx, 10); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(repo.recordedAttempts) != 1 {
		t.Fatalf("expected delivery result to be recorded despite cancelled context, got %+v", repo.recordedAttempts)
	}
	if len(repo.recordCtxWasCancelled) != 1 || repo.recordCtxWasCancelled[0] {
		t.Fatal("expected recording context to be non-cancelled so the row leaves 'processing'")
	}
}

func TestCreateOrderReturnsExistingOnPendingInsertConflict(t *testing.T) {
	repo := &fakePaymentRepository{
		referenceErr:   ErrPaymentOrderNotFound,
		referenceOrder: PaymentOrder{ID: "pay_winner", ExternalReference: "app-order-1"},
		failCreateOnce: true,
	}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	order, errs, err := service.CreateOrder(context.Background(), PaymentApp{ID: "app_test"}, CreatePaymentOrderInput{
		Provider:          "sonicpesa",
		Amount:            "10000",
		Currency:          "TZS",
		BuyerName:         "Customer",
		BuyerEmail:        "customer@example.com",
		BuyerPhone:        "255700000000",
		ExternalReference: "app-order-1",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if errs.Any() {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
	if order.ID != "pay_winner" {
		t.Fatalf("expected winner's row on insert conflict, got %q", order.ID)
	}
	// The loser must never reach the provider — otherwise two provider-side
	// orders exist and the buyer's payment on the orphan is never credited.
	if p.createCalled != 0 {
		t.Fatalf("expected provider create not called after losing the race, got %d", p.createCalled)
	}
}

func TestAttemptPayoutRefusedWhenAutomatedPayoutsDisabled(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "mobile_money", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	if _, err := service.AttemptPayout(context.Background(), "wd_test"); !errors.Is(err, ErrAutomatedPayoutsDisabled) {
		t.Fatalf("expected ErrAutomatedPayoutsDisabled, got %v", err)
	}
	if p.disburseCalled != 0 {
		t.Fatalf("expected no provider call while disabled, got %d", p.disburseCalled)
	}
	if len(repo.payoutClaimCalls) != 0 {
		t.Fatalf("expected no claim while disabled, got %+v", repo.payoutClaimCalls)
	}
}

func TestAttemptPayoutClaimsBeforeCallingProvider(t *testing.T) {
	repo := &fakePaymentRepository{
		withdrawal: PaymentWithdrawal{ID: "wd_test", Amount: "5000.00", Currency: "TZS", DestinationType: "mobile_money", Status: WithdrawalStatusApproved},
	}
	p := &fakePaymentProvider{
		name:         "sonicpesa",
		disburseResp: provider.DisburseResult{ProviderPayoutID: "payout_123", Status: provider.StatusPaid, ProviderStatus: "SUCCESS"},
	}
	opts := testServiceOptions()
	opts.AutomatedPayoutsEnabled = true
	service := NewService(repo, registryFor(p), testCipher, opts, nil)

	if _, err := service.AttemptPayout(context.Background(), "wd_test"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// The claim (approved → processing) must happen before Disburse so a
	// concurrent attempt fails the claim instead of sending money twice.
	if len(repo.payoutClaimCalls) != 1 || repo.payoutClaimCalls[0].id != "wd_test" || repo.payoutClaimCalls[0].provider != "sonicpesa" {
		t.Fatalf("expected single payout claim before disburse, got %+v", repo.payoutClaimCalls)
	}
	if p.disburseCalled != 1 {
		t.Fatalf("expected exactly one Disburse call, got %d", p.disburseCalled)
	}
}

func TestValidateWebhookURLRejectsInternalHosts(t *testing.T) {
	for _, raw := range []string{
		"http://10.0.0.5/hook",
		"http://192.168.1.10/hook",
		"http://172.16.0.9/hook",
		"http://127.0.0.1/hook",
		"http://[::1]/hook",
		"http://localhost/hook",
		"http://user:pass@example.com/hook",
		"ftp://example.com/hook",
		"not-a-url",
	} {
		if err := validateWebhookURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
	if err := validateWebhookURL("https://payments.example.com/webhooks/orders"); err != nil {
		t.Fatalf("expected public URL to be accepted, got %v", err)
	}
}

func TestIsZeroDecimal(t *testing.T) {
	for _, value := range []string{"", "0", "0.00", "0.000", " 0.00 "} {
		if !isZeroDecimal(value) {
			t.Fatalf("expected %q to count as zero", value)
		}
	}
	for _, value := range []string{"0.01", "5", "100.00"} {
		if isZeroDecimal(value) {
			t.Fatalf("expected %q to count as non-zero", value)
		}
	}
}

func paidTestOrder() PaymentOrder {
	return PaymentOrder{
		ID: "pay_test", AppID: "app_test", Provider: "sonicpesa",
		ProviderOrderID: "sp_123", ExternalReference: "app-order-1",
		Amount: "10000.00", Currency: "TZS", Status: provider.StatusPaid,
	}
}

func TestRefundOrderFallsBackToLocalReversalWhenProviderUnsupported(t *testing.T) {
	repo := &fakePaymentRepository{getOrder: paidTestOrder()}
	p := &fakePaymentProvider{name: "sonicpesa", refundErr: provider.ErrRefundNotSupported}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.RefundOrder(context.Background(), RefundOrderInput{OrderID: "pay_test", Reason: "customer request", RequestedBy: "admin@localhost"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if p.refundCalled != 1 {
		t.Fatalf("expected provider refund attempted once, got %d", p.refundCalled)
	}
	if len(repo.claimRefundInputs) != 1 {
		t.Fatalf("expected one refund claim before the provider call, got %+v", repo.claimRefundInputs)
	}
	if len(repo.confirmRefundCalls) != 1 {
		t.Fatalf("expected local confirm after unsupported provider, got %+v", repo.confirmRefundCalls)
	}
	if result.Order.Status != provider.StatusReversed {
		t.Fatalf("expected reversed order, got %q", result.Order.Status)
	}
	if result.Refund.Status != RefundStatusConfirmed || result.Refund.Amount != "10000.00" {
		t.Fatalf("expected confirmed full refund, got %+v", result.Refund)
	}
	// A payment.refunded event must be stored and fanned out signed like
	// any other payment.* event.
	found := false
	for _, event := range repo.events {
		if event.EventType == EventTypePaymentRefunded {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected payment.refunded event, got %+v", repo.events)
	}
	found = false
	for _, call := range repo.deliveryForEventCalls {
		if call.eventType == EventTypePaymentRefunded {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected refunded deliveries, got %+v", repo.deliveryForEventCalls)
	}
}

func TestRefundOrderFailsWithoutTouchingLedgerOnProviderError(t *testing.T) {
	repo := &fakePaymentRepository{getOrder: paidTestOrder()}
	p := &fakePaymentProvider{name: "sonicpesa", refundErr: errors.New("provider down")}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	if _, err := service.RefundOrder(context.Background(), RefundOrderInput{OrderID: "pay_test"}); !errors.Is(err, ErrRefundProviderFailed) {
		t.Fatalf("expected ErrRefundProviderFailed, got %v", err)
	}
	if len(repo.failRefundCalls) != 1 {
		t.Fatalf("expected failed claim recorded, got %+v", repo.failRefundCalls)
	}
	if len(repo.confirmRefundCalls) != 0 {
		t.Fatalf("expected no ledger confirm on provider failure, got %+v", repo.confirmRefundCalls)
	}
}

func TestRefundOrderRejectsCurrencyMismatch(t *testing.T) {
	repo := &fakePaymentRepository{getOrder: paidTestOrder()}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	if _, err := service.RefundOrder(context.Background(), RefundOrderInput{OrderID: "pay_test", Currency: "USD"}); !errors.Is(err, ErrRefundCurrencyMismatch) {
		t.Fatalf("expected ErrRefundCurrencyMismatch, got %v", err)
	}
	if len(repo.claimRefundInputs) != 0 || p.refundCalled != 0 {
		t.Fatal("expected rejection before claim and provider call")
	}
}

func TestHandleRefundWebhookConfirmsAsyncReversal(t *testing.T) {
	repo := &fakePaymentRepository{
		order: paidTestOrder(),
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        EventTypePaymentRefunded,
			ProviderEventID:  "rf_9",
			ProviderOrderID:  "sp_123",
			ProviderStatus:   "SUCCESS",
			NormalizedStatus: provider.StatusReversed,
			Amount:           "10000.00",
			Currency:         "TZS",
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"event":"refund.succeeded","order_id":"sp_123"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Processed || !result.OrderFound {
		t.Fatalf("expected processed refund with order, got %+v", result)
	}
	if len(repo.claimRefundInputs) != 1 || repo.claimRefundInputs[0].ProviderRefundID != "rf_9" {
		t.Fatalf("expected async claim keyed by provider refund id, got %+v", repo.claimRefundInputs)
	}
	if len(repo.confirmRefundCalls) != 1 {
		t.Fatalf("expected async confirm, got %+v", repo.confirmRefundCalls)
	}
	if len(repo.applied) != 0 {
		t.Fatalf("expected no credit-path apply for refunds, got %+v", repo.applied)
	}
}

func TestHandleRefundWebhookDuplicateReplayIsIdempotent(t *testing.T) {
	repo := &fakePaymentRepository{
		order:          paidTestOrder(),
		claimRefundErr: ErrAlreadyRefunded,
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        EventTypePaymentRefunded,
			ProviderEventID:  "rf_9",
			ProviderOrderID:  "sp_123",
			NormalizedStatus: provider.StatusReversed,
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"event":"refund.succeeded","order_id":"sp_123"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Processed || !result.Duplicate {
		t.Fatalf("expected idempotent duplicate result, got %+v", result)
	}
	if len(repo.confirmRefundCalls) != 0 {
		t.Fatalf("expected no second confirm on replay, got %+v", repo.confirmRefundCalls)
	}
}

func TestWebhookPayloadMatchesOrder(t *testing.T) {
	order := PaymentOrder{Amount: "10000.00", Currency: "TZS"}
	for _, tc := range []struct {
		name     string
		amount   string
		currency string
		want     bool
	}{
		{"exact", "10000.00", "TZS", true},
		{"formatting differs", "10000", "tzs", true},
		{"omitted", "", "", true},
		{"amount only match", "10000.0", "", true},
		{"amount mismatch", "9999.99", "TZS", false},
		{"currency mismatch", "10000.00", "USD", false},
		{"garbage amount", "abc", "TZS", false},
	} {
		if got := webhookPayloadMatchesOrder(order, tc.amount, tc.currency); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestFeePortionForRefund(t *testing.T) {
	// Full completion returns the exact remainder (no penny drift).
	if got := feePortionForRefund("250.00", "0", "10000.00", "10000.00", true); got != "250.00" {
		t.Fatalf("expected exact remainder 250.00, got %q", got)
	}
	if got := feePortionForRefund("250.00", "100.00", "6000.00", "10000.00", true); got != "150.00" {
		t.Fatalf("expected remainder 150.00, got %q", got)
	}
	// Partial refunds take the pro-rata share.
	if got := feePortionForRefund("250.00", "0", "4000.00", "10000.00", false); got != "100.00" {
		t.Fatalf("expected pro-rata 100.00, got %q", got)
	}
	// Never exceeds what is left, never pays when there is no fee.
	if got := feePortionForRefund("250.00", "200.00", "4000.00", "10000.00", false); got != "50.00" {
		t.Fatalf("expected clamped 50.00, got %q", got)
	}
	if got := feePortionForRefund("0.00", "0", "4000.00", "10000.00", false); got != "0.00" {
		t.Fatalf("expected 0.00 for fee-less order, got %q", got)
	}
}

func TestRefundOrderForwardsPartialAmountToClaim(t *testing.T) {
	repo := &fakePaymentRepository{getOrder: paidTestOrder()}
	p := &fakePaymentProvider{name: "sonicpesa", refundErr: provider.ErrRefundNotSupported}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.RefundOrder(context.Background(), RefundOrderInput{OrderID: "pay_test", Amount: "2500", Currency: "TZS"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(repo.claimRefundInputs) != 1 || repo.claimRefundInputs[0].Amount != "2500.00" {
		t.Fatalf("expected normalized partial claim 2500.00, got %+v", repo.claimRefundInputs)
	}
	if result.Refund.Amount != "2500.00" {
		t.Fatalf("expected partial refund 2500.00, got %+v", result.Refund)
	}
}

func TestRefundOrderRejectsOverRefund(t *testing.T) {
	repo := &fakePaymentRepository{getOrder: paidTestOrder(), claimRefundErr: ErrRefundAmountExceeded}
	p := &fakePaymentProvider{name: "sonicpesa"}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	if _, err := service.RefundOrder(context.Background(), RefundOrderInput{OrderID: "pay_test", Amount: "99999"}); !errors.Is(err, ErrRefundAmountExceeded) {
		t.Fatalf("expected ErrRefundAmountExceeded, got %v", err)
	}
	if p.refundCalled != 0 || len(repo.confirmRefundCalls) != 0 {
		t.Fatal("expected rejection before provider call and ledger confirm")
	}
}

func TestExpireOrdersEmitsExpiredEvents(t *testing.T) {
	repo := &fakePaymentRepository{
		expireCandidates: []PaymentOrder{
			{ID: "pay_old", AppID: "app_test", Provider: "sonicpesa", ProviderOrderID: "sp_old", Amount: "5000.00", Currency: "TZS", Status: provider.StatusPending},
		},
	}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)

	result, err := service.ExpireOrders(context.Background(), 50)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Expired != 1 {
		t.Fatalf("expected one expired order, got %+v", result)
	}
	foundEvent := false
	for _, event := range repo.events {
		if event.EventType == EventTypePaymentExpired {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("expected payment.expired event, got %+v", repo.events)
	}
	foundDelivery := false
	for _, call := range repo.deliveryForEventCalls {
		if call.eventType == EventTypePaymentExpired {
			foundDelivery = true
		}
	}
	if !foundDelivery {
		t.Fatalf("expected expired deliveries, got %+v", repo.deliveryForEventCalls)
	}
}

func TestLateWebhookForExpiredOrderHeldForReview(t *testing.T) {
	repo := &fakePaymentRepository{
		order: PaymentOrder{
			ID: "pay_old", AppID: "app_test", Provider: "sonicpesa",
			ProviderOrderID: "sp_old", Amount: "5000.00", Currency: "TZS",
			Status: provider.StatusExpired,
		},
	}
	p := &fakePaymentProvider{
		name: "sonicpesa",
		event: provider.WebhookEvent{
			EventType:        "payment.completed",
			ProviderOrderID:  "sp_old",
			NormalizedStatus: provider.StatusPaid,
			Amount:           "5000.00",
			Currency:         "TZS",
		},
	}
	service := NewService(repo, registryFor(p), testCipher, testServiceOptions(), nil)

	result, err := service.HandleProviderWebhook(context.Background(), "sonicpesa", http.Header{}, []byte(`{"order_id":"sp_old"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.Processed || !result.OrderFound {
		t.Fatalf("expected held-but-processed result, got %+v", result)
	}
	// Nothing may be credited or transitioned for an expired order.
	if len(repo.applied) != 0 {
		t.Fatalf("expected no ledger apply for expired order, got %+v", repo.applied)
	}
	held := false
	for _, message := range repo.marked {
		if strings.Contains(message, "manual review") {
			held = true
		}
	}
	if !held {
		t.Fatalf("expected manual-review mark, got %+v", repo.marked)
	}
}

func TestIdempotencyKeyReplaysSamePayload(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)
	ctx := context.Background()

	first, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", "hash-abc")
	if err != nil || !first.Claimed {
		t.Fatalf("expected fresh claim, got %+v, err %v", first, err)
	}
	if err := service.StoreIdempotencyResponse(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", 201, `{"data":{"id":"pay_1"}}`); err != nil {
		t.Fatalf("expected store success, got %v", err)
	}
	second, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", "hash-abc")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !second.Replay || second.Status != 201 || second.Body != `{"data":{"id":"pay_1"}}` {
		t.Fatalf("expected stored replay, got %+v", second)
	}
}

func TestIdempotencyKeyConflictsOnDifferentPayload(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)
	ctx := context.Background()

	if _, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", "hash-abc"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	second, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", "hash-DIFFERENT")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !second.Conflict || second.Replay {
		t.Fatalf("expected conflict, got %+v", second)
	}
}

func TestIdempotencyKeysAreScopedPerEndpoint(t *testing.T) {
	repo := &fakePaymentRepository{}
	service := NewService(repo, nil, testCipher, testServiceOptions(), nil)
	ctx := context.Background()

	if _, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointOrdersCreate, "key-1", "hash-abc"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	other, err := service.CheckIdempotencyKey(ctx, "app_test", IdempotencyEndpointAdminWithdrawalsCreate, "key-1", "hash-abc")
	if err != nil || !other.Claimed {
		t.Fatalf("expected fresh claim on different endpoint, got %+v, err %v", other, err)
	}
}
