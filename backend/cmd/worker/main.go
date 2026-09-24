// cmd/worker is optional — cmd/api already runs the same delivery/
// reconciliation/payout jobs inline (see internal/platform/httpserver's
// runBackgroundJobs), so a single deployed process is enough for a
// fully working gateway. Run this as a second process only if you want
// to scale those jobs independently of the API server; it's safe to run
// both at once (claim-based locking prevents duplicate processing).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"azsubay-payments-gateway/internal/modules/payments"
	"azsubay-payments-gateway/internal/modules/payments/providers"
	"azsubay-payments-gateway/internal/platform/config"
	azcrypto "azsubay-payments-gateway/internal/platform/crypto"
	"azsubay-payments-gateway/internal/platform/database"
	"azsubay-payments-gateway/internal/platform/observability"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(bootstrapLogger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("worker config load failed", "error", err)
		os.Exit(1)
	}

	logger := observability.NewLogger(cfg.App)
	slog.SetDefault(logger)
	logger.Info("starting azsubay payments gateway worker", "env", cfg.App.Env)

	db, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		logger.Error("worker database bootstrap failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	cipher, err := azcrypto.New(cfg.Security.EncryptionKey)
	if err != nil {
		logger.Error("worker encryption cipher init failed", "error", err)
		os.Exit(1)
	}

	paymentRepo := payments.NewPostgresRepository(db)
	paymentService := payments.NewService(paymentRepo, providers.Registry, cipher, payments.ServiceOptions{
		DeliverySigningSecret:          cfg.Payments.DeliverySigningSecret,
		DeliveryTimeout:                cfg.Payments.DeliveryTimeout,
		DeliveryMaxAttempts:            cfg.Payments.DeliveryMaxAttempts,
		ReconciliationStaleAfter:       cfg.Payments.ReconciliationStaleAfter,
		ReconciliationBatchSize:        cfg.Payments.ReconciliationBatchSize,
		AutomatedPayoutsEnabled:        cfg.Payments.AutomatedPayoutsEnabled,
		PayoutProvider:                 cfg.Payments.PayoutProvider,
		PayoutReconciliationStaleAfter: cfg.Payments.PayoutReconciliationStaleAfter,
		ProviderTimeout:                positiveDuration(cfg.HTTP.RequestTimeout-5*time.Second, 10*time.Second),
		OrderExpiryTTL:                 cfg.Payments.OrderTTL,
	}, logger)

	deliveryTicker := time.NewTicker(positiveDuration(cfg.Payments.DeliveryPollInterval, 30*time.Second))
	defer deliveryTicker.Stop()
	reconciliationTicker := time.NewTicker(positiveDuration(cfg.Payments.ReconciliationInterval, 5*time.Minute))
	defer reconciliationTicker.Stop()
	payoutTicker := time.NewTicker(positiveDuration(cfg.Payments.PayoutReconciliationInterval, 5*time.Minute))
	defer payoutTicker.Stop()
	expiryTicker := time.NewTicker(positiveDuration(cfg.Payments.ExpiryInterval, time.Minute))
	defer expiryTicker.Stop()

	processDeliveries := func() {
		result, err := paymentService.ProcessDueDeliveries(ctx, positiveInt(cfg.Payments.DeliveryBatchSize, 25))
		if err != nil {
			logger.Error("payment delivery processing failed", "error", err)
			return
		}
		if result.Claimed > 0 {
			logger.Info("payment deliveries processed", "claimed", result.Claimed, "delivered", result.Delivered, "retrying", result.Retrying, "failed", result.Failed)
		}
	}
	reconcilePayments := func() {
		result, err := paymentService.ReconcilePayments(ctx, positiveInt(cfg.Payments.ReconciliationBatchSize, 50))
		if err != nil {
			logger.Error("payment reconciliation failed", "error", err)
			return
		}
		if result.Scanned > 0 {
			logger.Info("payment reconciliation completed", "scanned", result.Scanned, "updated", result.Updated)
		}
	}
	reconcilePayouts := func() {
		if !cfg.Payments.AutomatedPayoutsEnabled {
			return
		}
		result, err := paymentService.ReconcilePayouts(ctx, positiveInt(cfg.Payments.ReconciliationBatchSize, 50))
		if err != nil {
			logger.Error("payout reconciliation failed", "error", err)
			return
		}
		if result.Scanned > 0 {
			logger.Info("payout reconciliation completed", "scanned", result.Scanned, "updated", result.Updated)
		}
	}
	expireOrders := func() {
		result, err := paymentService.ExpireOrders(ctx, positiveInt(cfg.Payments.ReconciliationBatchSize, 50))
		if err != nil {
			logger.Error("payment expiry failed", "error", err)
			return
		}
		if result.Expired > 0 {
			logger.Info("payment expiry completed", "expired", result.Expired)
		}
		if swept, err := paymentService.CleanupIdempotencyKeys(ctx); err != nil {
			logger.Error("idempotency cleanup failed", "error", err)
		} else if swept > 0 {
			logger.Info("idempotency cleanup completed", "deleted", swept)
		}
	}

	processDeliveries()
	reconcilePayments()
	reconcilePayouts()
	expireOrders()

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped cleanly")
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
