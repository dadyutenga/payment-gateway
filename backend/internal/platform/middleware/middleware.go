package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lipago/internal/platform/auth"
	"lipago/internal/platform/config"
	"lipago/internal/shared/httputil"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func Chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = randomID()
		}

		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID)))
	})
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			logger.Info("http_request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"query", r.URL.RawQuery,
				"status", recorder.status,
				"bytes", recorder.bytes,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						"request_id", RequestIDFromContext(r.Context()),
						"panic", recovered,
					)
					httputil.Error(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, `{"error":{"code":"timeout","message":"Request timed out."}}`)
	}
}

// MaxBytes caps request body size globally (HTTP_MAX_BODY_BYTES).
// Per-handler MaxBytesReader limits (e.g. webhooks) still apply inside.
func MaxBytes(n int64) func(http.Handler) http.Handler {
	if n <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, n)
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAuth(verifier *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(header, "Bearer ") {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing bearer token.", nil)
				return
			}

			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			claims, err := verifier.VerifyBearerToken(r.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "audience") {
					httputil.Error(w, http.StatusUnauthorized, "invalid_audience", "This token belongs to the other space — sign in through the matching login path.", nil)
					return
				}
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Invalid bearer token.", nil)
				return
			}

			ctx := auth.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SpaceChecker validates identity against the matching table on every
// request (revocation takes effect immediately, never waits for expiry).
type SpaceChecker interface {
	IsCustomerUser(ctx context.Context, userID string) (bool, error)
	IsAdminUser(ctx context.Context, userID string) (bool, error)
	CustomerEmailVerified(ctx context.Context, userID string) (bool, error)
}

// rateBucket is a minimal in-memory sliding-window limiter (per key).
type rateBucket struct {
	max    int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func newRateBucket(max int, window time.Duration) *rateBucket {
	return &rateBucket{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *rateBucket) allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// RateLimit caps requests per authenticated user (bearer subject), falling
// back to remote IP for pre-auth routes. Analytics reads are cheap but
// unbounded ranges are not — 60/min/user keeps dashboards snappy.
func RateLimit(perMinute int) func(http.Handler) http.Handler {
	bucket := newRateBucket(perMinute, time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIPFromRequest(r)
			if header := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(header, "Bearer ") {
				if claims, err := parseBearerSubject(header); err == nil && claims != "" {
					key = "user:" + claims
				}
			}
			if !bucket.allow(key) {
				httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many requests — slow down and retry.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCustomerAuth gates the customer space: customer-audience token +
// row in app.users. With requireVerified, unverified emails get
// 403 email_unverified (OTP/me endpoints use false so users can verify).
// otherSpace is the admin verifier: when the customer check fails but the
// token verifies as an admin token, the caller gets a clear
// invalid_audience error instead of a generic 401 (with distinct signing
// keys the signature check fails first, so this second probe is what
// identifies the token as belonging to the other space).
func RequireCustomerAuth(verifier, otherSpace *auth.Verifier, checker SpaceChecker, requireVerified bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(header, "Bearer ") {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing bearer token.", nil)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			claims, err := verifier.VerifyBearerToken(r.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "audience") || verifiesInOtherSpace(r, otherSpace, token) {
					httputil.Error(w, http.StatusUnauthorized, "invalid_audience", "Admin tokens are not accepted here — sign in at /login.", nil)
					return
				}
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Invalid bearer token.", nil)
				return
			}
			if claims.Audience != "" && claims.Audience != auth.AudienceCustomer {
				httputil.Error(w, http.StatusForbidden, "wrong_space", "Admin tokens are not accepted here — sign in at /login.", nil)
				return
			}
			ok, err := checker.IsCustomerUser(r.Context(), claims.Subject)
			if err != nil {
				httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to verify account.", nil)
				return
			}
			if !ok {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Account not found.", nil)
				return
			}
			if requireVerified {
				verified, err := checker.CustomerEmailVerified(r.Context(), claims.Subject)
				if err != nil {
					httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to verify account.", nil)
					return
				}
				if !verified {
					httputil.Error(w, http.StatusForbidden, "email_unverified", "Verify your email before continuing.", nil)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
}

// RequireAdminAuth gates the admin space: admin-audience token + row in
// app.admin_users, re-read per request so revocation is immediate.
// otherSpace is the customer verifier, used for the same clear
// invalid_audience signal as RequireCustomerAuth.
func RequireAdminAuth(verifier, otherSpace *auth.Verifier, checker SpaceChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(header, "Bearer ") {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing bearer token.", nil)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			claims, err := verifier.VerifyBearerToken(r.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "audience") || verifiesInOtherSpace(r, otherSpace, token) {
					httputil.Error(w, http.StatusUnauthorized, "invalid_audience", "Customer tokens are not accepted here — sign in at /admin/login.", nil)
					return
				}
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Invalid bearer token.", nil)
				return
			}
			if claims.Audience != "" && claims.Audience != auth.AudienceAdmin {
				httputil.Error(w, http.StatusForbidden, "wrong_space", "Customer tokens are not accepted here — sign in at /admin/login.", nil)
				return
			}
			ok, err := checker.IsAdminUser(r.Context(), claims.Subject)
			if err != nil {
				httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to verify admin privileges.", nil)
				return
			}
			if !ok {
				httputil.Error(w, http.StatusForbidden, "forbidden", "Insufficient privileges.", nil)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
}

// parseBearerSubject extracts the JWT subject without verifying (rate
// limiting only — auth decisions always verify the signature).
func parseBearerSubject(header string) (string, error) {
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("bad token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return claims.Subject, nil
}

func clientIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func verifiesInOtherSpace(r *http.Request, otherSpace *auth.Verifier, token string) bool {
	if otherSpace == nil || token == "" {
		return false
	}
	_, err := otherSpace.VerifyBearerToken(r.Context(), token)
	return err == nil
}

func RequireAdmin(check func(ctx context.Context, claims auth.Claims) (bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
				return
			}

			allowed, err := check(r.Context(), claims)
			if err != nil {
				httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to verify admin privileges.", nil)
				return
			}

			if !allowed {
				httputil.Error(w, http.StatusForbidden, "forbidden", "Insufficient privileges.", nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequestIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDKey).(string); ok {
		return value
	}
	return ""
}

func ClaimsFromContext(ctx context.Context) (auth.Claims, bool) {
	return auth.ClaimsFromContext(ctx)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(data []byte) (int, error) {
	n, err := s.ResponseWriter.Write(data)
	s.bytes += n
	return n, err
}

func randomID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
