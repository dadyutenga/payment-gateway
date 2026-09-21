package observability

import (
	"log/slog"
	"os"
	"strings"

	"azsubay-payments-gateway/internal/platform/config"
)

// NewLogger builds a JSON logger so application logs stay structured and
// machine-readable in both local containers and hosted environments.
func NewLogger(cfg config.AppConfig) *slog.Logger {
	level := new(slog.LevelVar)
	level.Set(parseLevel(cfg.LogLevel.String()))

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

func parseLevel(input string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
