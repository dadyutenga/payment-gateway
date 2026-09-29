package httpserver

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"lipago/internal/modules/analytics"
	"lipago/internal/modules/orgs"
	"lipago/internal/modules/payments"
	"lipago/internal/modules/payments/providers"
	"lipago/internal/platform/auth"
	"lipago/internal/platform/config"
	azcrypto "lipago/internal/platform/crypto"
	"lipago/internal/platform/database"
	"lipago/internal/platform/middleware"
	"lipago/internal/platform/observability"
	"lipago/internal/shared/httputil"

	"github.com/jackc/pgx"
)

// This mirrors the internal httpserver layout this project was extracted
// with: the payments module, health, and a minimal admin identity check.

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

	// Phase 1 Option A: two spaces, two token audiences, two lifetimes.
	// Customer tokens (aud=customer, AUTH_TOKEN_TTL) live in app.users;
	// admin tokens (aud=admin, AUTH_ADMIN_TOKEN_TTL, ideally a distinct
	// AUTH_ADMIN_JWT_SECRET) live in app.admin_users. Cross-use is
	// rejected by audience validation plus per-request DB checks.
	customerVerifier := auth.NewCustomerVerifier(cfg.Auth, logger)
	adminVerifier := auth.NewAdminVerifier(cfg.Auth, logger)
	// Email-verification gate for the customer space (dev escape hatch:
	// AUTH_REQUIRE_EMAIL_VERIFICATION=false while no mailer is wired).
	requireEmailVerified := cfg.Auth.RequireEmailVerification
	authService := auth.NewService(db, customerVerifier)
	authService.SetVerifiers(customerVerifier, adminVerifier)
	authService.SetAllowPublicRegister(cfg.Auth.AllowPublicRegister)
	authService.SetLoginRateLimits(cfg.Auth.LoginRateLimitPerMin, cfg.Auth.AdminLoginRateLimitPerMin)
	// OTP delivery stubs: log-only in every environment. Wire real
	// Mailer/SMSSender implementations (e.g. Beem Africa for TZ SMS) for
	// production — until then OTP requests return 503 otp_not_configured
	// rather than silently swallowing codes.
	authService.SetMailer(auth.NewLogMailer(logger))
	authService.SetSMSSender(auth.NewLogSMSSender(logger))

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
		LiveMaxTxnAmount:               cfg.Payments.LiveMaxTxnAmount,
		LiveDailyVolumeCap:             cfg.Payments.LiveDailyVolumeCap,
		PayerHashSecret:                cfg.Payments.PayerHashSecret,
	}, logger)
	// SMS success notifications, admin alerts, and an audit trail are all
	// optional integrations (SetSMSSender, SetNotifier, SetAuditWriter) —
	// none are wired here, so those features simply no-op. Wire your own
	// if you want them; the Service methods are narrow interfaces, not
	// concrete types.
	paymentHandler := payments.NewHandler(paymentService, cfg.Payments.WebhookMaxBodyBytes)
	paymentHandler.SetLogger(logger)

	orgRepo := orgs.NewPostgresRepository(db)
	orgService := orgs.NewService(orgRepo, logger)
	orgHandler := orgs.NewHandler(orgService, logger)
	paymentHandler.SetOrgService(orgService)

	analyticsRepo := analytics.NewPostgresRepository(db)
	analyticsService := analytics.NewService(analyticsRepo, logger)
	analyticsHandler := analytics.NewHandler(analyticsService, logger)
	analyticsHandler.SetOrgsService(orgService)

	// Admin identity is read from app.admin_users on every request by
	// RequireAdminAuth — never trusted from the token claim — so deleting
	// an admin row revokes access immediately. Admins are created via
	// cmd/seed-admin or an existing admin inviting another admin;
	// self-registration (app.users) can never grant admin.

	mux := http.NewServeMux()
	// ---- Customer space: public login paths ----
	mux.HandleFunc("POST /api/v1/auth/register", authService.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", authService.HandleLogin)
	// ---- Admin space: separate login path, audience, lifetime ----
	mux.HandleFunc("POST /api/v1/admin/auth/login", authService.HandleAdminLogin)
	mux.Handle("POST /api/v1/admin/auth/invite", middleware.Chain(http.HandlerFunc(authService.HandleAdminInvite), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	// OTP stays reachable before email verification so users can finish it.
	mux.Handle("POST /api/v1/auth/otp/request", middleware.Chain(http.HandlerFunc(authService.HandleOTPRequest), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	mux.Handle("POST /api/v1/auth/otp/verify", middleware.Chain(http.HandlerFunc(authService.HandleOTPVerify), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	mux.Handle("POST /api/v1/auth/password", middleware.Chain(http.HandlerFunc(authService.HandlePasswordChange), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	mux.Handle("GET /api/v1/auth/profile", middleware.Chain(http.HandlerFunc(authService.HandleGetOwnProfile), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	mux.Handle("PATCH /api/v1/auth/profile", middleware.Chain(http.HandlerFunc(authService.HandleUpdateOwnProfile), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	// Canonical identity endpoints per space.
	mux.Handle("GET /api/v1/auth/me", middleware.Chain(http.HandlerFunc(authService.HandleCustomerMe), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, false)))
	mux.Handle("GET /api/v1/admin/auth/me", middleware.Chain(http.HandlerFunc(authService.HandleAdminMe), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "up"
		dbCtx, cancel := context.WithTimeout(r.Context(), cfg.Database.HealthTimeout)
		defer cancel()
		if err := db.QueryRowEx(dbCtx, "SELECT 1", nil).Scan(new(int)); err != nil {
			dbStatus = "down"
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"service":         "lipago-payment-system",
			"status":          "ok",
			"env":             cfg.App.Env,
			"time":            time.Now().UTC(),
			"public_base_url": cfg.Payments.PublicBaseURL,
			"dependencies": map[string]any{
				"database": map[string]string{"status": dbStatus},
			},
		})
	})

	// Deprecated compat alias for the pre-split frontend (which calls
	// /api/v1/admin/me for every signed-in user). Accepts EITHER audience
	// and returns the matching shape; new code must use GET /api/v1/auth/me
	// (customer) or GET /api/v1/admin/auth/me (admin). Read-only: it grants
	// nothing and every mutating route still enforces its own space.
	mux.HandleFunc("GET /api/v1/admin/me", func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if header == "" || token == "" || strings.EqualFold(header, "Bearer") {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing bearer token.", nil)
			return
		}
		if claims, err := adminVerifier.VerifyBearerToken(r.Context(), token); err == nil {
			if ok, _ := authService.IsAdminUser(r.Context(), claims.Subject); ok {
				profile, profileErr := authService.GetAdminProfile(r.Context(), claims.Subject)
				if profileErr != nil {
					httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Admin account not found.", nil)
					return
				}
				httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{
					"email": profile.Email, "is_admin": true, "user": profile,
				}})
				return
			}
			httputil.Error(w, http.StatusForbidden, "forbidden", "Insufficient privileges.", nil)
			return
		}
		if claims, err := customerVerifier.VerifyBearerToken(r.Context(), token); err == nil {
			profile, profileErr := authService.GetUserProfile(r.Context(), claims.Subject)
			if profileErr != nil {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Account not found.", nil)
				return
			}
			httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"email": claims.Email, "is_admin": false, "user": profile,
				"email_verified": profile.EmailVerified, "phone": profile.Phone,
				"phone_verified": profile.PhoneVerified,
			}})
			return
		}
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Invalid bearer token.", nil)
	})

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
	}), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/apps/{id}/api-key", middleware.Chain(http.HandlerFunc(paymentHandler.GenerateAPIKey), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/balance", middleware.Chain(http.HandlerFunc(paymentHandler.GetAppBalance), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.ListLedgerEntries), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.ListLedgerEntries), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("PATCH /api/v1/admin/payments/apps/{id}/fees", middleware.Chain(http.HandlerFunc(paymentHandler.UpdateAppFees), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("DELETE /api/v1/admin/payments/apps/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.DeletePaymentApp), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

	// ---- Admin: app members ----
	mux.Handle("GET /api/v1/admin/payments/apps/{id}/members", middleware.Chain(http.HandlerFunc(paymentHandler.ListAppMembers), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/apps/{id}/members", middleware.Chain(http.HandlerFunc(paymentHandler.AddAppMember), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("DELETE /api/v1/admin/payments/apps/{id}/members/{userID}", middleware.Chain(http.HandlerFunc(paymentHandler.RemoveAppMember), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

	// ---- Merchant-facing (authenticated, not admin-gated — each handler
	// checks the caller's own app_id membership) ----
	mux.Handle("GET /api/v1/merchant/apps", middleware.Chain(http.HandlerFunc(paymentHandler.ListMyApps), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantCreateApp), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/merchant/apps/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantUpdateApp), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/balance", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantGetAppBalance), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/ledger", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListLedgerEntries), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/orders", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantSearchOrders), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/withdrawals", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListWithdrawals), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantCreateWithdrawal), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals/{withdrawalID}/approve", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantApproveWithdrawal), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/withdrawals/{withdrawalID}/reject", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRejectWithdrawal), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))

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
	}), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantUpdateWebhookEndpoint), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("DELETE /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantDeleteWebhookEndpoint), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}/test-send", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantTestWebhookEndpoint), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}/rotate-secret", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRotateWebhookEndpointSecret), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("/api/v1/merchant/apps/{id}/api-keys", middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			paymentHandler.MerchantListAPIKeys(w, r)
		case http.MethodPost:
			paymentHandler.MerchantCreateAPIKey(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/api-keys/{keyID}/rotate", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRotateAPIKey), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/merchant/apps/{id}/api-keys/{keyID}", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantUpdateAPIKeyLabel), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/api-keys/{keyID}/revoke", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantRevokeAPIKey), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/apps/{id}/deliveries", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantListDeliveries), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/merchant/apps/{id}/deliveries/{deliveryID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantReplayDelivery), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/merchant/orgs/{orgID}/limits-usage", middleware.Chain(http.HandlerFunc(paymentHandler.MerchantOrgLimitsUsage), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))

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
	}), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.GetOrganization), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.UpdateOrganization), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("DELETE /api/v1/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.DeleteOrganization), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/members", middleware.Chain(http.HandlerFunc(orgHandler.ListMembers), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/invites", middleware.Chain(http.HandlerFunc(orgHandler.InviteMember), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/accept", middleware.Chain(http.HandlerFunc(orgHandler.AcceptInvite), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/orgs/{orgID}/members/{userID}", middleware.Chain(http.HandlerFunc(orgHandler.ChangeMemberRole), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("DELETE /api/v1/orgs/{orgID}/members/{userID}", middleware.Chain(http.HandlerFunc(orgHandler.RemoveMember), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/leave", middleware.Chain(http.HandlerFunc(orgHandler.LeaveOrganization), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/kyc", middleware.Chain(http.HandlerFunc(orgHandler.SubmitKYC), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/kyc", middleware.Chain(http.HandlerFunc(orgHandler.GetKYCSubmission), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/kyc/attempts", middleware.Chain(http.HandlerFunc(orgHandler.ListKYCAttempts), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/notification-prefs", middleware.Chain(http.HandlerFunc(orgHandler.GetNotificationPrefs), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("PATCH /api/v1/orgs/{orgID}/notification-prefs", middleware.Chain(http.HandlerFunc(orgHandler.UpdateNotificationPrefs), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/logo", middleware.Chain(http.HandlerFunc(orgHandler.UploadOrgLogo), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/logo", middleware.Chain(http.HandlerFunc(orgHandler.ServeOrgLogo), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("POST /api/v1/orgs/{orgID}/kyc/document", middleware.Chain(http.HandlerFunc(orgHandler.UploadKYCDocument), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))
	mux.Handle("GET /api/v1/orgs/{orgID}/kyc/document", middleware.Chain(http.HandlerFunc(orgHandler.ServeKYCDocument), middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified)))

	// ---- Admin: KYC review queue, decisions, per-org live limits ----
	mux.Handle("GET /api/v1/admin/orgs/kyc-queue", middleware.Chain(http.HandlerFunc(orgHandler.ListKYCQueue), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/orgs/{orgID}", middleware.Chain(http.HandlerFunc(orgHandler.AdminOrgDetail), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/stats", middleware.Chain(http.HandlerFunc(orgHandler.PlatformStats), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

	// ---- Admin analytics (read-only; exports audited; per-user rate limit) ----
	adminAnalytics := func(h http.HandlerFunc) http.Handler {
		return middleware.Chain(h,
			middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService),
			middleware.RateLimit(60))
	}
	mux.Handle("GET /api/v1/admin/analytics/overview", adminAnalytics(analyticsHandler.Overview))
	mux.Handle("GET /api/v1/admin/analytics/providers", adminAnalytics(analyticsHandler.Providers))
	mux.Handle("GET /api/v1/admin/analytics/merchants/top", adminAnalytics(analyticsHandler.TopMerchants))
	mux.Handle("GET /api/v1/admin/analytics/merchants/signups", adminAnalytics(analyticsHandler.SignupFunnel))
	mux.Handle("GET /api/v1/admin/analytics/merchants/dormant", adminAnalytics(analyticsHandler.DormantMerchants))
	mux.Handle("GET /api/v1/admin/analytics/merchants/churn-risk", adminAnalytics(analyticsHandler.ChurnRiskMerchants))
	mux.Handle("GET /api/v1/admin/analytics/failures", adminAnalytics(analyticsHandler.Failures))
	mux.Handle("GET /api/v1/admin/analytics/withdrawals", adminAnalytics(analyticsHandler.Withdrawals))
	mux.Handle("GET /api/v1/admin/analytics/webhooks", adminAnalytics(analyticsHandler.Webhooks))
	mux.Handle("GET /api/v1/admin/analytics/ops/stuck-orders", adminAnalytics(analyticsHandler.StuckOrders))
	mux.Handle("GET /api/v1/admin/analytics/ops/unreconciled", adminAnalytics(analyticsHandler.Unreconciled))
	mux.Handle("GET /api/v1/admin/analytics/ops/negative-balances", adminAnalytics(analyticsHandler.NegativeBalances))
	mux.Handle("GET /api/v1/admin/analytics/export/{report}", adminAnalytics(analyticsHandler.Export))

	// ---- Merchant analytics (customer space, org-scoped server-side) ----
	merchantAnalytics := func(h http.HandlerFunc) http.Handler {
		return middleware.Chain(h,
			middleware.RequireCustomerAuth(customerVerifier, adminVerifier, authService, requireEmailVerified),
			middleware.RateLimit(60))
	}
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/overview", merchantAnalytics(analyticsHandler.MerchantOverview))
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/methods", merchantAnalytics(analyticsHandler.MerchantMethods))
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/peak-hours", merchantAnalytics(analyticsHandler.MerchantPeakHours))
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/customers", merchantAnalytics(analyticsHandler.MerchantCustomers))
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/failures", merchantAnalytics(analyticsHandler.MerchantFailures))
	mux.Handle("GET /api/v1/orgs/{orgID}/analytics/apps", merchantAnalytics(analyticsHandler.MerchantApps))
	mux.Handle("GET /api/v1/orgs/{orgID}/settlements", merchantAnalytics(analyticsHandler.MerchantSettlements))
	mux.Handle("POST /api/v1/admin/orgs/{orgID}/kyc/approve", middleware.Chain(http.HandlerFunc(orgHandler.ApproveKYC), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/orgs/{orgID}/kyc/reject", middleware.Chain(http.HandlerFunc(orgHandler.RejectKYC), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/orgs/{orgID}/kyc/document", middleware.Chain(http.HandlerFunc(orgHandler.AdminServeKYCDocument), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("PATCH /api/v1/admin/orgs/{orgID}/limits", middleware.Chain(http.HandlerFunc(orgHandler.UpdateOrgLiveLimits), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

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
	}), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/approve", middleware.Chain(http.HandlerFunc(paymentHandler.ApproveWithdrawal), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/reject", middleware.Chain(http.HandlerFunc(paymentHandler.RejectWithdrawal), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/mark-paid", middleware.Chain(http.HandlerFunc(paymentHandler.MarkWithdrawalPaid), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/mark-failed", middleware.Chain(http.HandlerFunc(paymentHandler.MarkWithdrawalFailed), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/withdrawals/{id}/retry-payout", middleware.Chain(http.HandlerFunc(paymentHandler.RetryWithdrawalPayout), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

	// ---- Admin: provider accounts (credentials encrypted at rest) ----
	mux.Handle("GET /api/v1/admin/payments/providers", middleware.Chain(http.HandlerFunc(paymentHandler.ListProviderAccounts), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/providers", middleware.Chain(http.HandlerFunc(paymentHandler.CreateProviderAccount), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("PATCH /api/v1/admin/payments/providers/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.UpdateProviderAccount), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("DELETE /api/v1/admin/payments/providers/{id}", middleware.Chain(http.HandlerFunc(paymentHandler.DeleteProviderAccount), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/providers/{id}/set-default", middleware.Chain(http.HandlerFunc(paymentHandler.SetDefaultProviderAccount), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

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
	}), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/orders", middleware.Chain(http.HandlerFunc(paymentHandler.SearchPaymentOrders), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/orders/{id}/refund", middleware.Chain(http.HandlerFunc(paymentHandler.RefundOrder), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/events", middleware.Chain(http.HandlerFunc(paymentHandler.ListPaymentEvents), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/events/{eventID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.ReplayEvent), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/deliveries", middleware.Chain(http.HandlerFunc(paymentHandler.ListWebhookDeliveries), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/deliveries/process", middleware.Chain(http.HandlerFunc(paymentHandler.ProcessDeliveries), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/deliveries/{deliveryID}/replay", middleware.Chain(http.HandlerFunc(paymentHandler.ReplayFailedDelivery), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("POST /api/v1/admin/payments/reconcile", middleware.Chain(http.HandlerFunc(paymentHandler.ReconcilePayments), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))
	mux.Handle("GET /api/v1/admin/payments/metrics", middleware.Chain(http.HandlerFunc(paymentHandler.PaymentMetrics), middleware.RequireAdminAuth(adminVerifier, customerVerifier, authService)))

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
	// Analytics rollups: yesterday + today on every reconciliation tick
	// (rebuildable cache — safe with cmd/worker running alongside).
	refreshAnalytics := func() {
		now := time.Now().UTC()
		if err := a.paymentService.RefreshAnalyticsRollups(a.ctx, now.Add(-24*time.Hour), now); err != nil {
			a.logger.Error("analytics rollup refresh failed", "error", err)
		}
	}

	processDeliveries()
	reconcilePayments()
	reconcilePayouts()
	expireOrders()
	refreshAnalytics()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-deliveryTicker.C:
			processDeliveries()
		case <-reconciliationTicker.C:
			reconcilePayments()
			refreshAnalytics()
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
