package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"azsubay-payments-gateway/internal/platform/httpserver"
)

// Root entrypoint so the backend can be started with:
//
//	cd backend
//	go run .
//
// This is the same as `go run ./cmd/api`.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("starting azsubay payments gateway", "env", os.Getenv("APP_ENV"))

	app, err := httpserver.New(ctx)
	if err != nil {
		slog.Error("application bootstrap failed", "error", err)
		os.Exit(1)
	}
	defer app.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Start()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			app.Logger().Error("graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		app.Logger().Info("server stopped cleanly")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.Logger().Error("server exited unexpectedly", "error", err)
			os.Exit(1)
		}
	}
}
