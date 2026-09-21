package payments

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"azsubay-payments-gateway/internal/modules/payments/provider"
	"azsubay-payments-gateway/internal/platform/middleware"
	"azsubay-payments-gateway/internal/shared/httputil"
)

type Handler struct {
	service      *Service
	maxBodyBytes int64
}

func NewHandler(service *Service, maxBodyBytes int64) *Handler {
	return &Handler{
		service:      service,
		maxBodyBytes: maxBodyBytes,
	}
}

type createOrderHTTPInput struct {
	Provider          string          `json:"provider"`
	Amount            json.RawMessage `json:"amount"`
	Currency          string          `json:"currency"`
	BuyerName         string          `json:"buyer_name"`
	BuyerEmail        string          `json:"buyer_email"`
	BuyerPhone        string          `json:"buyer_phone"`
	ExternalReference string          `json:"external_reference"`
	Metadata          map[string]any  `json:"metadata"`
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	app, ok := h.authenticateApp(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	var in createOrderHTTPInput
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	amount, err := amountFromJSON(in.Amount)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Amount must be a number or numeric string.", nil)
		return
	}

	order, vErrs, err := h.service.CreateOrder(r.Context(), app, CreatePaymentOrderInput{
		Provider:          in.Provider,
		Amount:            amount,
		Currency:          in.Currency,
		BuyerName:         in.BuyerName,
		BuyerEmail:        in.BuyerEmail,
		BuyerPhone:        in.BuyerPhone,
		ExternalReference: in.ExternalReference,
		Metadata:          in.Metadata,
	})
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "payment_provider_error", "Unable to create payment order.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your payment order input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": order})
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	app, ok := h.authenticateApp(w, r)
	if !ok {
		return
	}

	order, err := h.service.GetOrder(r.Context(), app, r.PathValue("paymentID"))
	if err != nil {
		if errors.Is(err, ErrPaymentOrderNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment order not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load payment order.", nil)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": order})
}

func (h *Handler) RefreshOrder(w http.ResponseWriter, r *http.Request) {
	app, ok := h.authenticateApp(w, r)
	if !ok {
		return
	}

	order, err := h.service.RefreshOrder(r.Context(), app, r.PathValue("paymentID"))
	if err != nil {
		switch {
		case errors.Is(err, ErrPaymentOrderNotFound):
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment order not found.", nil)
		case errors.Is(err, ErrProviderOrderMissing):
			httputil.Error(w, http.StatusConflict, "provider_order_missing", "Payment order has not been registered with a provider.", nil)
		case errors.Is(err, ErrUnknownProvider):
			httputil.Error(w, http.StatusUnprocessableEntity, "provider_not_supported", "Payment provider is not supported.", nil)
		default:
			httputil.Error(w, http.StatusBadGateway, "payment_provider_error", "Unable to refresh payment order.", map[string]string{"error": err.Error()})
		}
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": order})
}

func (h *Handler) CreateApp(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	var input CreatePaymentAppInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	result, vErrs, err := h.service.CreateApp(r.Context(), input)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to create payment app.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your payment app input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": result})
}

// GenerateAPIKey is admin-only: it issues a new key for an app (revoking
// any prior active key) and returns the raw key exactly once.
func (h *Handler) GenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	rawKey, err := h.service.GenerateAPIKey(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "api_key_generation_failed", "Unable to generate API key.", nil)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"api_key": rawKey}})
}

func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListApps(r.Context(), limit, offset)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list payment apps.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

// ---------- Ledger, balances, fees ----------

func (h *Handler) GetAppBalance(w http.ResponseWriter, r *http.Request) {
	balance, err := h.service.GetAppBalance(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load app balance.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": balance})
}

// ListLedgerEntries serves both the per-app route (/payments/apps/{id}/ledger)
// and the global route (/payments/ledger?app_id=) — the path value wins when
// present, otherwise the app_id query param is used (empty means "all apps").
func (h *Handler) ListLedgerEntries(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	appID := r.PathValue("id")
	if appID == "" {
		appID = r.URL.Query().Get("app_id")
	}

	result, err := h.service.ListLedgerEntries(r.Context(), LedgerEntryListFilter{
		AppID:  appID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list ledger entries.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) UpdateAppFees(w http.ResponseWriter, r *http.Request) {
	var in UpdateAppFeesInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	app, vErrs, err := h.service.UpdateAppFees(r.Context(), r.PathValue("id"), in)
	if err != nil {
		if errors.Is(err, ErrPaymentAppNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment app not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "update_failed", "Unable to update app fees.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your fee input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": app})
}

func (h *Handler) DeletePaymentApp(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	app, err := h.service.DeletePaymentApp(r.Context(), claims.Subject, claims.Email, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrPaymentAppNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment app not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "delete_failed", "Unable to delete app.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": app})
}

// ---------- Withdrawals ----------

func (h *Handler) ListWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListWithdrawals(r.Context(), WithdrawalListFilter{
		AppID:  r.URL.Query().Get("app_id"),
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list withdrawals.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

type createWithdrawalHTTPInput struct {
	AppID              string         `json:"app_id"`
	Amount             string         `json:"amount"`
	Currency           string         `json:"currency"`
	DestinationType    string         `json:"destination_type"`
	DestinationDetails map[string]any `json:"destination_details"`
	Notes              string         `json:"notes"`
}

func (h *Handler) CreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	var in createWithdrawalHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	withdrawal, vErrs, err := h.service.CreateWithdrawal(r.Context(), claims.Subject, CreateWithdrawalInput{
		AppID:              in.AppID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		DestinationType:    in.DestinationType,
		DestinationDetails: in.DestinationDetails,
		Notes:              in.Notes,
	})
	if err != nil {
		if errors.Is(err, ErrInsufficientBalance) {
			httputil.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", "This app's available balance doesn't cover that amount.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to create withdrawal.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your withdrawal input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": withdrawal})
}

func (h *Handler) withdrawalTransitionError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, ErrWithdrawalNotFound):
		httputil.Error(w, http.StatusNotFound, "not_found", "Withdrawal not found.", nil)
	case errors.Is(err, ErrInvalidWithdrawalTransition):
		httputil.Error(w, http.StatusConflict, "invalid_transition", "This withdrawal isn't in a state that can be "+action+".", nil)
	case errors.Is(err, ErrInsufficientBalance):
		httputil.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", "This app's available balance no longer covers this withdrawal.", nil)
	default:
		httputil.Error(w, http.StatusInternalServerError, "update_failed", "Unable to update withdrawal.", map[string]string{"error": err.Error()})
	}
}

func (h *Handler) ApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	withdrawal, err := h.service.ApproveWithdrawal(r.Context(), r.PathValue("id"), claims.Subject)
	if err != nil {
		h.withdrawalTransitionError(w, err, "approved")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

func (h *Handler) RejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawal, err := h.service.RejectWithdrawal(r.Context(), r.PathValue("id"))
	if err != nil {
		h.withdrawalTransitionError(w, err, "rejected")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

func (h *Handler) MarkWithdrawalPaid(w http.ResponseWriter, r *http.Request) {
	withdrawal, err := h.service.MarkWithdrawalPaid(r.Context(), r.PathValue("id"))
	if err != nil {
		h.withdrawalTransitionError(w, err, "marked paid")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

type markWithdrawalFailedInput struct {
	Notes string `json:"notes"`
}

func (h *Handler) MarkWithdrawalFailed(w http.ResponseWriter, r *http.Request) {
	var in markWithdrawalFailedInput
	_ = json.NewDecoder(r.Body).Decode(&in)

	withdrawal, err := h.service.MarkWithdrawalFailed(r.Context(), r.PathValue("id"), in.Notes)
	if err != nil {
		h.withdrawalTransitionError(w, err, "marked failed")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

// RetryWithdrawalPayout re-attempts an automated payout dispatch for a
// withdrawal that's still "approved" (either automation is off and an
// admin wants to try it, or a prior attempt recorded a failure_reason
// without ever reaching the provider). Safe to call repeatedly — see
// Service.AttemptPayout's idempotency note.
func (h *Handler) RetryWithdrawalPayout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	withdrawal, err := h.service.AttemptPayout(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrWithdrawalNotFound):
			httputil.Error(w, http.StatusNotFound, "not_found", "Withdrawal not found.", nil)
			return
		case errors.Is(err, ErrInvalidWithdrawalTransition):
			httputil.Error(w, http.StatusConflict, "invalid_transition", "This withdrawal isn't in a state that can be dispatched for payout.", nil)
			return
		case errors.Is(err, ErrProviderDoesNotSupportPayouts):
			httputil.Error(w, http.StatusUnprocessableEntity, "unsupported_provider", "This payment provider doesn't support automated payouts yet.", nil)
			return
		}
		// The Disburse call itself failed — RecordPayoutFailure has already
		// saved the reason, so re-fetch to hand the frontend the latest
		// failure_reason rather than just an error string.
		if current, getErr := h.service.GetWithdrawal(r.Context(), id); getErr == nil {
			withdrawal = current
		}
		httputil.JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": map[string]any{"code": "payout_failed", "message": "Payout attempt failed — the withdrawal remains approved and can be retried."},
			"data":  withdrawal,
		})
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": withdrawal})
}

// ---------- Merchant identity & access ----------

func (h *Handler) ListAppMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.service.ListAppMembers(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list app members.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": members})
}

type addAppMemberInput struct {
	Email string `json:"email"`
}

func (h *Handler) AddAppMember(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	var in addAppMemberInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	member, vErrs, err := h.service.AddAppMemberByEmail(r.Context(), r.PathValue("id"), in.Email, claims.Subject)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			httputil.Error(w, http.StatusNotFound, "user_not_found", "No AZsubay account found for that email. They need to sign up first.", nil)
		case errors.Is(err, ErrAlreadyMember):
			httputil.Error(w, http.StatusConflict, "already_member", "This person already has access to this app.", nil)
		default:
			httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to add app member.", map[string]string{"error": err.Error()})
		}
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": member})
}

func (h *Handler) RemoveAppMember(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RemoveAppMember(r.Context(), r.PathValue("id"), r.PathValue("userID")); err != nil {
		if errors.Is(err, ErrAppMemberNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "App member not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "delete_failed", "Unable to remove app member.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"removed": true}})
}

func (h *Handler) ListProviderAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.service.ListProviderAccounts(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list payment providers.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": accounts})
}

func (h *Handler) CreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	var in CreateProviderAccountInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	account, vErrs, err := h.service.CreateProviderAccount(r.Context(), claims.Subject, claims.Email, in)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to create payment provider.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your payment provider input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": account})
}

func (h *Handler) UpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	var in UpdateProviderAccountInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	account, vErrs, err := h.service.UpdateProviderAccount(r.Context(), claims.Subject, claims.Email, r.PathValue("id"), in)
	if err != nil {
		if errors.Is(err, ErrPaymentProviderAccountNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment provider not found.", nil)
			return
		}
		httputil.Error(w, http.StatusUnprocessableEntity, "update_failed", "Unable to update payment provider.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your payment provider input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": account})
}

func (h *Handler) DeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	if err := h.service.DeleteProviderAccount(r.Context(), claims.Subject, claims.Email, r.PathValue("id")); err != nil {
		if errors.Is(err, ErrPaymentProviderAccountNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment provider not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "delete_failed", "Unable to delete payment provider.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true}})
}

func (h *Handler) SetDefaultProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	if err := h.service.SetDefaultProviderAccount(r.Context(), claims.Subject, claims.Email, r.PathValue("id")); err != nil {
		if errors.Is(err, ErrPaymentProviderAccountNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment provider not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "update_failed", "Unable to set default payment provider.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updated": true}})
}

func (h *Handler) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	var input CreatePaymentWebhookEndpointInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	result, vErrs, err := h.service.CreateWebhookEndpoint(r.Context(), input)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to create payment webhook endpoint.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your webhook endpoint input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": result})
}

func (h *Handler) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.service.ListWebhookEndpoints(r.Context(), r.URL.Query().Get("app_id"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list webhook endpoints.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": endpoints})
}

func (h *Handler) ReplayEvent(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ReplayEvent(r.Context(), r.PathValue("eventID"))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "replay_failed", "Unable to replay payment event.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) RefundOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.service.RefundOrder(r.Context(), r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, ErrPaymentOrderNotFound):
			httputil.Error(w, http.StatusNotFound, "not_found", "Payment order not found.", nil)
		case errors.Is(err, ErrOrderNotRefundable):
			httputil.Error(w, http.StatusConflict, "not_refundable", "Only a paid order can be refunded.", nil)
		case errors.Is(err, ErrAlreadyRefunded):
			httputil.Error(w, http.StatusConflict, "already_refunded", "This order has already been refunded.", nil)
		default:
			httputil.Error(w, http.StatusInternalServerError, "refund_failed", "Unable to refund order.", map[string]string{"error": err.Error()})
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": order})
}

func (h *Handler) SearchPaymentOrders(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.SearchPaymentOrders(r.Context(), PaymentOrderSearchFilter{
		Query:             r.URL.Query().Get("q"),
		AppID:             r.URL.Query().Get("app_id"),
		Provider:          r.URL.Query().Get("provider"),
		Status:            provider.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Phone:             r.URL.Query().Get("phone"),
		PaymentID:         r.URL.Query().Get("payment_id"),
		ProviderOrderID:   r.URL.Query().Get("provider_order_id"),
		ExternalReference: r.URL.Query().Get("external_reference"),
		Limit:             limit,
		Offset:            offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "search_failed", "Unable to search payment orders.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) ListPaymentEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}
	processed, ok := parseOptionalBool(w, r, "processed")
	if !ok {
		return
	}
	hasError, ok := parseOptionalBool(w, r, "has_error")
	if !ok {
		return
	}
	signatureValid, ok := parseOptionalBool(w, r, "signature_valid")
	if !ok {
		return
	}

	result, err := h.service.ListPaymentEvents(r.Context(), PaymentEventListFilter{
		Provider:        r.URL.Query().Get("provider"),
		EventType:       r.URL.Query().Get("event_type"),
		PaymentOrderID:  r.URL.Query().Get("payment_order_id"),
		ProviderOrderID: r.URL.Query().Get("provider_order_id"),
		Status:          provider.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Processed:       processed,
		HasError:        hasError,
		SignatureValid:  signatureValid,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list payment events.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListWebhookDeliveries(r.Context(), PaymentWebhookDeliveryListFilter{
		Status:         r.URL.Query().Get("status"),
		EventID:        r.URL.Query().Get("event_id"),
		EndpointID:     r.URL.Query().Get("endpoint_id"),
		PaymentOrderID: r.URL.Query().Get("payment_order_id"),
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list payment webhook deliveries.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) ReplayFailedDelivery(w http.ResponseWriter, r *http.Request) {
	if err := h.service.ReplayFailedDelivery(r.Context(), r.PathValue("deliveryID")); err != nil {
		if errors.Is(err, ErrPaymentWebhookDeliveryNotFound) {
			httputil.Error(w, http.StatusNotFound, "not_found", "Failed payment webhook delivery not found.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "replay_failed", "Unable to replay payment webhook delivery.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"replayed": true,
		},
	})
}

func (h *Handler) PaymentMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.service.PaymentMetrics(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "metrics_failed", "Unable to load payment metrics.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": metrics})
}

func (h *Handler) ProcessDeliveries(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Limit must be a positive integer.", nil)
			return
		}
		if parsed > 100 {
			parsed = 100
		}
		limit = parsed
	}

	result, err := h.service.ProcessDueDeliveries(r.Context(), limit)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "process_failed", "Unable to process payment webhook deliveries.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) ReconcilePayments(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Limit must be a positive integer.", nil)
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	result, err := h.service.ReconcilePayments(r.Context(), limit)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "reconcile_failed", "Unable to reconcile payments.", map[string]string{"error": err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) ProviderWebhook(w http.ResponseWriter, r *http.Request) {
	providerName := strings.TrimSpace(r.PathValue("provider"))
	if providerName == "" {
		httputil.Error(w, http.StatusNotFound, "provider_not_found", "Payment provider not found.", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to read request body.", nil)
		return
	}

	result, err := h.service.HandleProviderWebhook(r.Context(), providerName, r.Header, rawBody)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownProvider):
			httputil.Error(w, http.StatusNotFound, "provider_not_found", "Payment provider not found.", nil)
		case errors.Is(err, ErrWebhookVerificationFailed):
			httputil.Error(w, http.StatusUnauthorized, "invalid_signature", "Payment webhook signature could not be verified.", nil)
		case errors.Is(err, ErrWebhookParseFailed):
			httputil.Error(w, http.StatusBadRequest, "invalid_webhook", "Payment webhook payload is invalid.", nil)
		default:
			httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to process payment webhook.", nil)
		}
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"received":    true,
		"event_id":    result.EventID,
		"duplicate":   result.Duplicate,
		"processed":   result.Processed,
		"order_found": result.OrderFound,
	})
}

func (h *Handler) authenticateApp(w http.ResponseWriter, r *http.Request) (PaymentApp, bool) {
	apiKey := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if apiKey == "" {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing payment API key.", nil)
		return PaymentApp{}, false
	}

	app, err := h.service.AuthenticateApp(r.Context(), apiKey)
	if err != nil {
		if errors.Is(err, ErrPaymentAppUnauthorized) {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Invalid payment API key.", nil)
			return PaymentApp{}, false
		}
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to authenticate payment app.", nil)
		return PaymentApp{}, false
	}

	return app, true
}

func amountFromJSON(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		unquoted, err := strconv.Unquote(trimmed)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(unquoted), nil
	}
	return trimmed, nil
}

func parsePagination(w http.ResponseWriter, r *http.Request, defaultLimit, maxLimit int) (int, int, bool) {
	limit := defaultLimit
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Limit must be a positive integer.", nil)
			return 0, 0, false
		}
		if parsed > maxLimit {
			parsed = maxLimit
		}
		limit = parsed
	}

	offset := 0
	if rawOffset := strings.TrimSpace(r.URL.Query().Get("offset")); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Offset must be zero or a positive integer.", nil)
			return 0, 0, false
		}
		offset = parsed
	}

	return limit, offset, true
}

func parseOptionalBool(w http.ResponseWriter, r *http.Request, key string) (*bool, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", key+" must be true or false.", nil)
		return nil, false
	}
	return &parsed, true
}

// ---------- Merchant-facing routes (Phase 3) ----------
// Authenticated but not admin-gated — each handler checks the caller's own
// membership against the path's app id, the same way admin handlers read
// r.PathValue("id") themselves rather than through generic middleware
// (middleware.RequireAdmin's check closure has no access to path values).

// requireMembership returns the caller's user id and whether they're a
// member of the given app, writing the appropriate error response itself
// when not (401 if unauthenticated, 403 if not a member).
func (h *Handler) requireMembership(w http.ResponseWriter, r *http.Request, appID string) (string, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return "", false
	}

	isMember, err := h.service.IsAppMember(r.Context(), claims.Subject, appID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to verify app access.", nil)
		return "", false
	}
	if !isMember {
		httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this app.", nil)
		return "", false
	}

	return claims.Subject, true
}

func (h *Handler) ListMyApps(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}

	apps, err := h.service.ListAppsForUser(r.Context(), claims.Subject)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list your apps.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": apps})
}

func (h *Handler) MerchantGetAppBalance(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireMembership(w, r, appID); !ok {
		return
	}

	balance, err := h.service.GetAppBalance(r.Context(), appID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load app balance.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": balance})
}

func (h *Handler) MerchantListLedgerEntries(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireMembership(w, r, appID); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListLedgerEntries(r.Context(), LedgerEntryListFilter{AppID: appID, Limit: limit, Offset: offset})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list ledger entries.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) MerchantSearchOrders(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireMembership(w, r, appID); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	// AppID is forced to the path value regardless of any client-supplied
	// query param — a merchant must never be able to widen this to another
	// app's orders.
	result, err := h.service.SearchPaymentOrders(r.Context(), PaymentOrderSearchFilter{
		AppID:  appID,
		Query:  r.URL.Query().Get("q"),
		Status: provider.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Phone:  r.URL.Query().Get("phone"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "search_failed", "Unable to search orders.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) MerchantListWithdrawals(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, ok := h.requireMembership(w, r, appID); !ok {
		return
	}

	limit, offset, ok := parsePagination(w, r, 50, 200)
	if !ok {
		return
	}

	result, err := h.service.ListWithdrawals(r.Context(), WithdrawalListFilter{AppID: appID, Limit: limit, Offset: offset})
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "list_failed", "Unable to list withdrawals.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) MerchantCreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	userID, ok := h.requireMembership(w, r, appID)
	if !ok {
		return
	}

	var in createWithdrawalHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}

	withdrawal, vErrs, err := h.service.CreateWithdrawal(r.Context(), userID, CreateWithdrawalInput{
		AppID:              appID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		DestinationType:    in.DestinationType,
		DestinationDetails: in.DestinationDetails,
		Notes:              in.Notes,
	})
	if err != nil {
		if errors.Is(err, ErrInsufficientBalance) {
			httputil.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", "This app's available balance doesn't cover that amount.", nil)
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "create_failed", "Unable to create withdrawal.", map[string]string{"error": err.Error()})
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your withdrawal input.", vErrs)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{"data": withdrawal})
}
