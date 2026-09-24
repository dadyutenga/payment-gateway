package httpserver

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"azsubay-payments-gateway/internal/modules/orgs"
	"azsubay-payments-gateway/internal/modules/payments"
	"azsubay-payments-gateway/internal/modules/payments/providers"
	"azsubay-payments-gateway/internal/platform/auth"
	"azsubay-payments-gateway/internal/platform/config"
	azcrypto "azsubay-payments-gateway/internal/platform/crypto"
	"azsubay-payments-gateway/internal/platform/database"
	"azsubay-payments-gateway/internal/platform/middleware"
	"azsubay-payments-gateway/internal/platform/observability"
	"azsubay-payments-gateway/internal/shared/httputil"

	"github.com/jackc/pgx"
)

// This mirrors azsubayec-backend/internal/platform/httpserver, trimmed to
// only what the standalone Payments Gateway needs: the payments module,
// health, and a minimal admin identity check. See that repo for the full
// multi-module version (products, SMS, users, OAuth-provider, ...).

type App struct {
	cfg    config.Config
	logger *slog.Logger
	server *http.Server
	db     *pgx.ConnPool
	ctx    context.Context

	paymentService *payments.Service
}

func New(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := observability.NewLogger(cfg.App)

	db, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}

	authVerifier := auth.NewVerifier(cfg.Auth, logger)
	authService := auth.NewService(db, authVerifier, cfg.Admin.Emails)
	authService.SetAllowPublicRegister(cfg.Auth.AllowPublicRegister)

	cipher, err := azcrypto.New(cfg.Security.EncryptionKey)
	if err != nil {
		db.Close()
		return nil, err
	}

	paymentRepo := payments.NewPostgresRepository(db)
	// The provider call timeout stays below the request timeout so a slow
	// provider surfaces as a retryable error instead of a request killed
	// mid-flight (which orphans provider-side orders).
	providerTimeout := cfg.HTTP.RequestTimeout - 5*time.Second
	if providerTimeout < 5*time.Second {
		providerTimeout = 5 * time.Second
	}
	paymentService := payments.NewService(paymentRepo, providers.Registry, cipher, payments.ServiceOptions{
		DeliverySigningSecret:          cfg.Payments.DeliverySigningSecret,
		DeliveryTimeout:                cfg.Payments.DeliveryTimeout,
		DeliveryMaxAttempts:            cfg.Payments.DeliveryMaxAttempts,
		ReconciliationStaleAfter:       cfg.Payments.ReconciliationStaleAfter,
		ReconciliationBatchSize:        cfg.Payments.ReconciliationBatchSize,
		AutomatedPayoutsEnabled:        cfg.Payments.AutomatedPayoutsEnabled,
		PayoutProvider:                 cfg.Payments.PayoutProvider,
		PayoutReconciliationStaleAfter: cfg.Payments.PayoutReconciliationStaleAfter,
		ProviderTimeout:                providerTimeout,
		OrderExpiryTTL:                 cfg.Payments.OrderTTL,
	}, logger)
	// SMS success notifications, admin alerts, and an audit trail are all
	// optional integrations in the full AZSUBAY backend (SetSMSSender,
	// SetNotifier, SetAuditWriter) — none are wired here, so those features
	// simply no-op. Wire your own if you want them; the Service methods are
	// narrow interfaces, not concrete AZSUBAY types.
	paymentHandler := payments.NewHandler(paymentService, cfg.Payments.WebhookMaxBodyBytes)
	paymentHandler.SetLogger(logger)

	orgRepo := orgs.NewPostgresRepository(db)
	orgService := orgs.NewService(orgRepo, logger)
	orgHandler := orgs.NewHandler(orgService, logger)
	paymentHandler.SetOrgService(orgService)

	// Admin privileges are stored in app.users and read from the database on
	// every request — never trusted from the token claim. ADMIN_EMAILS only
	// bootstraps the first administrators as they register, and revoking
	// is_admin takes effect immediately (no waiting for token expiry).
	adminCheck := func(ctx context.Context, claims auth.Claims) (bool, error) {
		return authService.IsAdmin(ctx, claims.Subject)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", authService.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", authService.HandleLogin)

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "up"
		dbCtx, cancel := context.WithTimeout(r.Context(), cfg.Database.HealthTimeout)
		defer cancel()
		if err := db.QueryRowEx(dbCtx, "SELECT 1", nil).Scan(new(int)); err != nil {
			dbStatus = "down"
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"service":         "azsubay-payments-gateway",
			"status":          "ok",
			"env":             cfg.App.Env,
			"time":            time.Now().UTC(),
			"public_base_url": cfg.Payments.PublicBaseURL,
			"dependencies": map[string]any{
				"database": map[string]string{"status": dbStatus},
			},
		})
	})

	// A lightweight identity endpoint so the frontend can show "signed in
	// as X" / "not an admin" without guessing from 401s alone.
	mux.Handle("GET /api/v1/admin/me", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
			return
		}
		isAdmin, err := adminCheck(r.Context(), claims)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to check admin status.", nil)
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"email":    claims.Email,
			"is_admin": isAdmin,
		}})
	}), middleware.RequireAuth(authVerifier)))

	// ---- Public: order creation/status + provider webhooks ----
	mux.HandleFunc("POST /api/v1/payments/orders", paymentHandler.CreateOrder)
	mux.HandleFunc("GET /api/v1/payments/orders/{paymentID}", paymentHandler.GetOrder)
	mux.HandleFunc("POST /api/v1/payments/orders/{paymentID}/refresh", paymentHandler.RefreshOrder)
	mux.HandleFunc("POST /api/v1/payments/webhooks/{provider}", paymentHandler.ProviderWebhook)

	// ---- Admin: apps/keys ----
	mux.Handle("/api/v1/admin/payments/apps", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.ListApps(w, r)
		case http.MethodPost:
			paymentHandler.CreateApp(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/apps/{id}/api-key", middleware.Chain(http.HandlerFunc(paymentHandler.GenerateAPIKey), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/balance", middleware.Chain(http.HandlerFunc(paymentHandler.GetAppBalance), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.ListLedgerEntries), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.ListLedgerEntries), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("PATCH /api/v1/admin/payments/apps/{id}/fees", middleware.Chain(http.HandlerFunc(paymentHandler.UpdateAppFees), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("DELETE /api/v1/admin/payments/apps/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.DeletePaymentApp), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))

	// ---- Admin: app members ----
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/members", middleware.Chain(http.HandlerFunc(paymentHandler.ListAppMembers), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/apps/{id}/members", middleware.Chain(http.HandlerFunc(paymentHandler.AddAppMember), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("DELETE /api/v1/admin/payments/apps/{id}/members/{userID}", middleware.Chain(http.HandlerFunc(paymentHandler.RemoveAppMember), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))

	// ---- Merchant-facing (authenticated, not admin-gated — each handler
	// checks the caller's own app_id membership) ----
	mux.Handle("GET /api/v1/merchant/apps", middleware.Chain(http.HandlerFunc(paymentHandler.ListMyApps), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/balance", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantGetAppBalance), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListLedgerEntries), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/orders", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantSearchOrders), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/withdrawals", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListWithdrawals), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantCreateWithdrawal), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals/{withdrawalID}/approve", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantApproveWithdrawal), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals/{withdrawalID}/reject", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRejectWithdrawal), middleware.RequireAuth(authVerifier)))

	// ---- Merchant self-service: webhook endpoints, API keys, deliveries ----
	// All scoped to the path app id via membership on the caller's session —
	// handlers never trust a client-supplied app id.
	mux.Handle("/api/v1/merchant/apps/{id}/webhook-endpoints", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.MerchantListWebhookEndpoints(w, r)
		case http.MethodPost:
			paymentHandler.MerchantCreateWebhookEndpoint(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier)))
	mux.Handle("PATCH /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantUpdateWebhookEndpoint), middleware.RequireAuth(authVerifier)))
	mux.Handle("DELETE /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantDeleteWebhookEndpoint), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}/test-send", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantTestWebhookEndpoint), middleware.RequireAuth(authVerifier)))
	mux.Handle("/api/v1/merchant/apps/{id}/api-keys", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.MerchantListAPIKeys(w, r)
		case http.MethodPost:
			paymentHandler.MerchantCreateAPIKey(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/api-keys/{keyID}/rotate", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRotateAPIKey), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/api-keys/{keyID}/revoke", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRevokeAPIKey), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/deliveries", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListDeliveries), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/deliveries/{deliveryID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantReplayDelivery), middleware.RequireAuth(authVerifier)))

	// ---- Organizations & roles (session-authenticated; role checks run
	// inside the handlers/services against the member's own row) ----
	mux.Handle("/api/v1/orgs", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			orgHandler.ListMyOrganizations(w, r)
		case http.MethodPost:
			orgHandler.CreateOrganization(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.GetOrganization), middleware.RequireAuth(authVerifier)))
	mux.Handle("PATCH /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.UpdateOrganization), middleware.RequireAuth(authVerifier)))
	mux.Handle("DELETE /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.DeleteOrganization), middleware.RequireAuth(authVerifier)))
	mux.Handle("GET /api/v1/orgs/{orgID}/members", middleware.Chain(http.HandlerFunc(orgHandler.ListMembers), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/orgs/{orgID}/invites", middleware.Chain(http.HandlerFunc(orgHandler.InviteMember), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/orgs/{orgID}/accept", middleware.Chain(http.HandlerFunc(orgHandler.AcceptInvite), middleware.RequireAuth(authVerifier)))
	mux.Handle("PATCH /api/v1/orgs/{orgID}/members/{userID}", middleware.Chain(http.HandlerFunc(orgHandler.ChangeMemberRole), middleware.RequireAuth(authVerifier)))
	mux.Handle("DELETE /api/v1/orgs/{orgID}/members/{userID}", middleware.Chain(http.HandlerFunc(orgHandler.RemoveMember), middleware.RequireAuth(authVerifier)))
	mux.Handle("POST /api/v1/orgs/{orgID}/leave", middleware.Chain(http.HandlerFunc(orgHandler.LeaveOrganization), middleware.RequireAuth(authVerifier)))

	// ---- Admin: withdrawals ----
	mux.Handle("/api/v1/admin/payments/withdrawals", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.ListWithdrawals(w, r)
		case http.MethodPost:
			paymentHandler.CreateWithdrawal(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/approve", middleware.Chain(http.HandlerFunc(paymentHandler.ApproveWithdrawal), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/reject", middleware.Chain(http.HandlerFunc(paymentHandler.RejectWithdrawal), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/mark-paid", middleware.Chain(http.HandlerFunc(paymentHandler.MarkWithdrawalPaid), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/mark-failed", middleware.Chain(http.HandlerFunc(paymentHandler.MarkWithdrawalFailed), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/retry-payout", middleware.Chain(http.HandlerFunc(paymentHandler.RetryWithdrawalPayout), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))

	// ---- Admin: provider accounts (credentials encrypted at rest) ----
	mux.Handle("GET /api/v1/admin/payments/providers", middleware.Chain(http.HandlerFunc(paymentHandler.ListProviderAccounts), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/providers", middleware.Chain(http.HandlerFunc(paymentHandler.CreateProviderAccount), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("PATCH /api/v1/admin/payments/providers/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.UpdateProviderAccount), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("DELETE /api/v1/admin/payments/providers/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.DeleteProviderAccount), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/providers/{id}/set-default", middleware.Chain(http.HandlerFunc(paymentHandler.SetDefaultProviderAccount), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))

	// ---- Admin: webhook endpoints, orders, events, deliveries, reconciliation, metrics ----
	mux.Handle("/api/v1/admin/payments/webhook-endpoints", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.ListWebhookEndpoints(w, r)
		case http.MethodPost:
			paymentHandler.CreateWebhookEndpoint(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/orders", middleware.Chain(http.HandlerFunc(paymentHandler.SearchPaymentOrders), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/orders/{id}/refund", middleware.Chain(http.HandlerFunc(paymentHandler.RefundOrder), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/events", middleware.Chain(http.HandlerFunc(paymentHandler.ListPaymentEvents), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/events/{eventID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.ReplayEvent), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/deliveries", middleware.Chain(http.HandlerFunc(paymentHandler.ListWebhookDeliveries), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/deliveries/process", middleware.Chain(http.HandlerFunc(paymentHandler.ProcessDeliveries), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/deliveries/{deliveryID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.ReplayFailedDelivery), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("POST /api/v1/admin/payments/reconcile", middleware.Chain(http.HandlerFunc(paymentHandler.ReconcilePayments), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))
	mux.Handle("GET /api/v1/admin/payments/metrics", middleware.Chain(http.HandlerFunc(paymentHandler.PaymentMetrics), middleware.RequireAuth(authVerifier), middleware.RequireAdmin(adminCheck)))

	handler := middleware.Chain(
		mux,
		middleware.CORS(cfg.CORS),
		middleware.SecurityHeaders,
		middleware.RequestID,
		middleware.Logging(logger),
		middleware.Recovery(logger),
		middleware.MaxBytes(cfg.HTTP.MaxBodyBytes),
		middleware.Timeout(cfg.HTTP.RequestTimeout),
	)

	server := &http.Server{
		Addr:              ":" + cfg.HTTP.Port,
		Handler:           handler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	return &App{
		cfg:            cfg,
		logger:         logger,
		server:         server,
		db:             db,
		ctx:            ctx,
		paymentService: paymentService,
	}, nil
}

// runBackgroundJobs runs the same delivery/reconciliation jobs cmd/worker
// runs, so a single deployed process (just the API server) is enough to get
// a fully working gateway. Intervals and batch sizes come from the same env
// vars as the worker (PAYMENTS_DELIVERY_POLL_INTERVAL,
// PAYMENTS_RECONCILIATION_INTERVAL, ...). Run cmd/worker as a separate
// process too if you want to scale delivery/reconciliation independently of
// the API — both are safe to run at once (deliveries use claim-based locking;
// payment updates serialize on per-order row locks with idempotent ledger
// inserts).
func (a *App) runBackgroundJobs() {
	deliveryTicker := time.NewTicker(positiveDuration(a.cfg.Payments.DeliveryPollInterval, 30*time.Second))
	defer deliveryTicker.Stop()
	reconciliationTicker := time.NewTicker(positiveDuration(a.cfg.Payments.ReconciliationInterval, 5*time.Minute))
	defer reconciliationTicker.Stop()
	payoutTicker := time.NewTicker(positiveDuration(a.cfg.Payments.PayoutReconciliationInterval, 5*time.Minute))
	defer payoutTicker.Stop()
	expiryTicker := time.NewTicker(positiveDuration(a.cfg.Payments.ExpiryInterval, time.Minute))
	defer expiryTicker.Stop()

	processDeliveries := func() {
		if result, err := a.paymentService.ProcessDueDeliveries(a.ctx, positiveInt(a.cfg.Payments.DeliveryBatchSize, 25)); err != nil {
			a.logger.Error("payment delivery processing failed", "error", err)
		} else if result.Claimed > 0 {
			a.logger.Info("payment deliveries processed", "claimed", result.Claimed, "delivered", result.Delivered, "retrying", result.Retrying, "failed", result.Failed)
		}
	}
	reconcilePayments := func() {
		if result, err := a.paymentService.ReconcilePayments(a.ctx, positiveInt(a.cfg.Payments.ReconciliationBatchSize, 50)); err != nil {
			a.logger.Error("payment reconciliation failed", "error", err)
		} else if result.Scanned > 0 {
			a.logger.Info("payment reconciliation completed", "scanned", result.Scanned, "updated", result.Updated)
		}
	}
	reconcilePayouts := func() {
		if !a.cfg.Payments.AutomatedPayoutsEnabled {
			return
		}
		if result, err := a.paymentService.ReconcilePayouts(a.ctx, positiveInt(a.cfg.Payments.ReconciliationBatchSize, 50)); err != nil {
			a.logger.Error("payout reconciliation failed", "error", err)
		} else if result.Scanned > 0 {
			a.logger.Info("payout reconciliation completed", "scanned", result.Scanned, "updated", result.Updated)
		}
	}
	expireOrders := func() {
		if result, err := a.paymentService.ExpireOrders(a.ctx, positiveInt(a.cfg.Payments.ReconciliationBatchSize, 50)); err != nil {
			a.logger.Error("payment expiry failed", "error", err)
		} else if result.Expired > 0 {
			a.logger.Info("payment expiry completed", "expired", result.Expired)
		}
		if swept, err := a.paymentService.CleanupIdempotencyKeys(a.ctx); err != nil {
			a.logger.Error("idempotency cleanup failed", "error", err)
		} else if swept > 0 {
			a.logger.Info("idempotency cleanup completed", "deleted", swept)
		}
	}

	processDeliveries()
	reconcilePayments()
	reconcilePayouts()
	expireOrders()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-deliveryTicker.C:
			processDeliveries()
		case <-reconciliationTicker.C:
			reconcilePayments()
		case <-payoutTicker.C:
			reconcilePayouts()
		case <-expiryTicker.C:
			expireOrders()
		}
	}
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func (a *App) Start() error {
	a.logger.Info("starting http server", "addr", a.server.Addr, "env", a.cfg.App.Env)
	go a.runBackgroundJobs()
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, a.cfg.App.ShutdownTimeout)
	defer cancel()
	return a.server.Shutdown(shutdownCtx)
}

func (a *App) Close() {
	if a.db != nil {
		a.db.Close()
	}
}

func (a *App) Logger() *slog.Logger {
	return a.logger
}
