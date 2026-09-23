package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration required to boot the standalone
// AZSUBAY Payments Gateway. This is a trimmed copy of the full AZSUBAY
// backend's config — only what the Payments module actually needs (see
// that repo's internal/platform/config for the full version). No Redis,
// no OAuth-provider config: this package doesn't include those modules.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Database DatabaseConfig
	Auth     AuthConfig
	Admin    AdminConfig
	Payments PaymentConfig
	CORS     CORSConfig
	Security SecurityConfig
}

type AppConfig struct {
	Name            string
	Env             string
	LogLevel        slogLevel
	ShutdownTimeout time.Duration
}

type HTTPConfig struct {
	Port              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
	MaxBodyBytes      int64
}

type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int32
	MinOpenConns    int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthTimeout   time.Duration
}

type AuthConfig struct {
	JWTSecret string
	TokenTTL  time.Duration
	// AllowPublicRegister keeps POST /api/v1/auth/register open to anyone.
	// Default false: only the very first account (empty users table) may
	// self-register as a bootstrap; afterwards registration is closed and
	// additional accounts must be created by an operator. Leaving this open
	// with ADMIN_EMAILS copied from an example lets anyone claim admin by
	// registering a listed address.
	AllowPublicRegister bool
}

// AdminConfig is deliberately simple: a fixed allowlist of emails, set via
// env var, rather than a database-backed admin-role system. That's enough
// for "who's allowed to run this Payments Gateway" — if you need a richer
// admin model (roles, invitations, audit trail of admin changes), that's
// what the full AZSUBAY backend's oauth/users modules are for.
type AdminConfig struct {
	Emails []string
}

type CORSConfig struct {
	AllowedOrigins []string
}

type SecurityConfig struct {
	// EncryptionKey is a base64-encoded 32-byte AES-256 key used to encrypt
	// payment provider credentials at rest.
	EncryptionKey string
}

type PaymentConfig struct {
	PublicBaseURL            string
	WebhookMaxBodyBytes      int64
	DeliveryTimeout          time.Duration
	DeliveryMaxAttempts      int
	DeliveryBatchSize        int
	DeliveryPollInterval     time.Duration
	DeliverySigningSecret    string
	ReconciliationStaleAfter time.Duration
	ReconciliationBatchSize  int
	ReconciliationInterval   time.Duration
	// AutomatedPayoutsEnabled gates automated payout dispatch. Defaults to
	// false — SonicPesa's disbursement API contract should be verified
	// against your own account before enabling this.
	AutomatedPayoutsEnabled        bool
	PayoutProvider                 string
	PayoutReconciliationStaleAfter time.Duration
	PayoutReconciliationInterval   time.Duration
}

type slogLevel string

func (l slogLevel) String() string {
	return string(l)
}

// Load validates environment variables and returns a fully initialized
// config. See .env.example for every variable and what it's for.
func Load() (Config, error) {
	LoadDotEnv(".env")
	cfg := Config{
		App: AppConfig{
			Name:            getEnv("APP_NAME", "azsubay-payments-gateway"),
			Env:             getEnv("APP_ENV", "development"),
			LogLevel:        slogLevel(getEnv("LOG_LEVEL", "INFO")),
			ShutdownTimeout: mustDuration("APP_SHUTDOWN_TIMEOUT", "15s"),
		},
		HTTP: HTTPConfig{
			Port:              getEnv("APP_PORT", "8080"),
			ReadTimeout:       mustDuration("HTTP_READ_TIMEOUT", "10s"),
			ReadHeaderTimeout: mustDuration("HTTP_READ_HEADER_TIMEOUT", "5s"),
			WriteTimeout:      mustDuration("HTTP_WRITE_TIMEOUT", "15s"),
			IdleTimeout:       mustDuration("HTTP_IDLE_TIMEOUT", "60s"),
			RequestTimeout:    mustDuration("HTTP_REQUEST_TIMEOUT", "15s"),
			MaxBodyBytes:      mustInt64("HTTP_MAX_BODY_BYTES", 1<<20),
		},
		Database: DatabaseConfig{
			URL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
			MaxOpenConns:    mustInt32("DATABASE_MAX_OPEN_CONNS", 20),
			MinOpenConns:    mustInt32("DATABASE_MIN_OPEN_CONNS", 2),
			MaxConnLifetime: mustDuration("DATABASE_MAX_CONN_LIFETIME", "30m"),
			MaxConnIdleTime: mustDuration("DATABASE_MAX_CONN_IDLE_TIME", "5m"),
			HealthTimeout:   mustDuration("DATABASE_HEALTH_TIMEOUT", "3s"),
		},
		Auth: AuthConfig{JWTSecret: strings.TrimSpace(os.Getenv("AUTH_JWT_SECRET")), TokenTTL: mustDuration("AUTH_TOKEN_TTL", "24h"), AllowPublicRegister: mustBool("AUTH_ALLOW_PUBLIC_REGISTER", false)},
		Admin: AdminConfig{
			Emails: splitCSVLower(os.Getenv("ADMIN_EMAILS")),
		},
		CORS: CORSConfig{
			AllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")),
		},
		Security: SecurityConfig{
			EncryptionKey: strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY")),
		},
		Payments: PaymentConfig{
			PublicBaseURL:                  strings.TrimRight(getEnv("PAYMENTS_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
			WebhookMaxBodyBytes:            mustInt64("PAYMENTS_WEBHOOK_MAX_BODY_BYTES", 1<<20),
			DeliveryTimeout:                mustDuration("PAYMENTS_DELIVERY_TIMEOUT", "10s"),
			DeliveryMaxAttempts:            mustInt("PAYMENTS_DELIVERY_MAX_ATTEMPTS", 10),
			DeliveryBatchSize:              mustInt("PAYMENTS_DELIVERY_BATCH_SIZE", 25),
			DeliveryPollInterval:           mustDuration("PAYMENTS_DELIVERY_POLL_INTERVAL", "30s"),
			DeliverySigningSecret:          getEnv("PAYMENTS_DELIVERY_SIGNING_SECRET", "development-payment-delivery-secret"),
			ReconciliationStaleAfter:       mustDuration("PAYMENTS_RECONCILIATION_STALE_AFTER", "15m"),
			ReconciliationBatchSize:        mustInt("PAYMENTS_RECONCILIATION_BATCH_SIZE", 50),
			ReconciliationInterval:         mustDuration("PAYMENTS_RECONCILIATION_INTERVAL", "5m"),
			AutomatedPayoutsEnabled:        mustBool("PAYMENTS_AUTOMATED_PAYOUTS_ENABLED", false),
			PayoutProvider:                 getEnv("PAYMENTS_PAYOUT_PROVIDER", "sonicpesa"),
			PayoutReconciliationStaleAfter: mustDuration("PAYMENTS_PAYOUT_RECONCILIATION_STALE_AFTER", "15m"),
			PayoutReconciliationInterval:   mustDuration("PAYMENTS_PAYOUT_RECONCILIATION_INTERVAL", "5m"),
		},
	}

	var validationErrs []string
	if cfg.Database.URL == "" {
		validationErrs = append(validationErrs, "DATABASE_URL is required")
	}
	if cfg.Security.EncryptionKey == "" {
		validationErrs = append(validationErrs, "APP_ENCRYPTION_KEY is required")
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		validationErrs = append(validationErrs, "AUTH_JWT_SECRET must be at least 32 characters")
	}
	if len(cfg.Admin.Emails) == 0 {
		validationErrs = append(validationErrs, "ADMIN_EMAILS is required (comma-separated list of emails allowed to sign in as admin)")
	}
	if strings.EqualFold(cfg.App.Env, "production") && cfg.Payments.DeliverySigningSecret == "development-payment-delivery-secret" {
		validationErrs = append(validationErrs, "PAYMENTS_DELIVERY_SIGNING_SECRET is required in production")
	}
	if cfg.Payments.PublicBaseURL != "" {
		if parsed, err := url.ParseRequestURI(cfg.Payments.PublicBaseURL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			validationErrs = append(validationErrs, "PAYMENTS_PUBLIC_BASE_URL must be a valid http(s) URL")
		}
	}
	if len(validationErrs) > 0 {
		return Config{}, errors.New(strings.Join(validationErrs, "; "))
	}

	return cfg, nil
}

// LoadDotEnv keeps local development configuration self-contained without
// requiring a runtime dependency. Process environment values always win.
func LoadDotEnv(path string) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, strings.Trim(strings.TrimSpace(value), "\"'"))
	}
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func mustDuration(key, fallback string) time.Duration {
	value := getEnv(key, fallback)
	duration, err := time.ParseDuration(value)
	if err != nil {
		panic(fmt.Sprintf("invalid duration for %s: %v", key, err))
	}
	return duration
}

func mustInt32(key string, fallback int32) int32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		panic(fmt.Sprintf("invalid int32 for %s: %v", key, err))
	}
	return int32(parsed)
}

func mustInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("invalid int64 for %s: %v", key, err))
	}
	return parsed
}

func mustInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("invalid int for %s: %v", key, err))
	}
	return parsed
}

func mustBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		panic(fmt.Sprintf("invalid bool for %s: %v", key, err))
	}
	return parsed
}

func splitCSV(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitCSVLower(input string) []string {
	parts := splitCSV(input)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.ToLower(part))
	}
	return out
}
