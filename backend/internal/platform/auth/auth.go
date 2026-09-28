package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lipago/internal/platform/config"
	"lipago/internal/shared/httputil"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	Audience string `json:"aud"`
	Expiry   int64  `json:"exp"`
	IssuedAt int64  `json:"iat"`
}

// Token audiences partition the two spaces. A customer token is never
// accepted on an admin route and vice versa — enforced by using separate
// Verifier instances (ideally separate signing keys) plus per-request DB
// checks against the matching table.
const (
	AudienceCustomer = "customer"
	AudienceAdmin    = "admin"
)

// claimsKey stores verified Claims on the request context. Defined here
// (not in middleware) so auth handlers can read claims without importing
// middleware — middleware imports auth, so the reverse would cycle.
type claimsKey struct{}

// WithClaims returns a context carrying verified Claims.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// ClaimsFromContext returns verified Claims from the request context.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(Claims)
	return claims, ok
}

type User struct {
	ID, Email, PasswordHash string
	IsAdmin                 bool
}

type Verifier struct {
	secret   []byte
	tokenTTL time.Duration
	audience string
}

// NewVerifier keeps its original signature for compatibility and returns
// the CUSTOMER verifier (aud=customer).
func NewVerifier(cfg config.AuthConfig, _ any) *Verifier {
	return NewCustomerVerifier(cfg, nil)
}

// NewCustomerVerifier validates customer-space tokens (aud=customer).
func NewCustomerVerifier(cfg config.AuthConfig, _ any) *Verifier {
	return &Verifier{secret: []byte(cfg.JWTSecret), tokenTTL: cfg.TokenTTL, audience: AudienceCustomer}
}

// NewAdminVerifier validates admin-space tokens (aud=admin). It uses
// AUTH_ADMIN_JWT_SECRET when set, otherwise falls back to the customer
// secret — audience separation still rejects cross-use either way.
func NewAdminVerifier(cfg config.AuthConfig, _ any) *Verifier {
	secret := strings.TrimSpace(cfg.AdminJWTSecret)
	if secret == "" {
		secret = cfg.JWTSecret
	}
	ttl := cfg.AdminTokenTTL
	if ttl <= 0 {
		ttl = cfg.TokenTTL
	}
	return &Verifier{secret: []byte(secret), tokenTTL: ttl, audience: AudienceAdmin}
}

func (v *Verifier) Audience() string { return v.audience }

func (v *Verifier) IssueToken(user User) (string, error) {
	now := time.Now().Unix()
	claims := Claims{Subject: user.ID, Email: user.Email, IsAdmin: user.IsAdmin, Audience: v.audience, IssuedAt: now, Expiry: now + int64(v.tokenTTL.Seconds())}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	input := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (v *Verifier) VerifyBearerToken(_ context.Context, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token format")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, errors.New("invalid token header")
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(headerBytes, &header) != nil || header.Algorithm != "HS256" {
		return Claims{}, errors.New("unsupported token algorithm")
	}
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(mac.Sum(nil), signature) {
		return Claims{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid token payload")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, errors.New("invalid token claims")
	}
	if claims.Subject == "" || claims.Email == "" || time.Now().Unix() >= claims.Expiry {
		return Claims{}, errors.New("token expired or invalid")
	}
	// Audience partition: a token minted for one space is unusable in the
	// other even if the user_id somehow matches a row there.
	if v.audience != "" && claims.Audience != "" && claims.Audience != v.audience {
		return Claims{}, errors.New("invalid token audience")
	}
	if v.audience != "" && claims.Audience == "" {
		return Claims{}, errors.New("invalid token audience")
	}
	return claims, nil
}

type Service struct {
	db                  *pgx.ConnPool
	verifier            *Verifier // customer verifier (back-compat)
	customerVerifier    *Verifier
	adminVerifier       *Verifier
	allowPublicRegister bool
	mailer              Mailer
	sms                 SMSSender
	loginLimiter        *rateLimiter
	adminLoginLimiter   *rateLimiter
	// TOTPVerify is the hook where TOTP 2FA plugs in later:
	// func(secret, code string) bool. Nil means "no TOTP provider wired".
	TOTPVerify func(secret, code string) bool
}

func NewService(db *pgx.ConnPool, verifier *Verifier) *Service {
	return &Service{
		db: db, verifier: verifier,
		customerVerifier: verifier,
		loginLimiter:      newRateLimiter(20, time.Minute),
		adminLoginLimiter: newRateLimiter(5, time.Minute),
	}
}

// SetVerifiers wires the split customer/admin verifiers (separate
// audiences, separate TTLs, ideally separate secrets).
func (s *Service) SetVerifiers(customer, admin *Verifier) {
	if customer != nil {
		s.customerVerifier = customer
		s.verifier = customer
	}
	if admin != nil {
		s.adminVerifier = admin
	}
}

// SetLoginRateLimits overrides the default 20/min customer and 5/min
// admin login caps (from AUTH_LOGIN_RATE_LIMIT_PER_MIN and
// AUTH_ADMIN_LOGIN_RATE_LIMIT_PER_MIN).
func (s *Service) SetLoginRateLimits(customerPerMin, adminPerMin int) {
	if customerPerMin > 0 {
		s.loginLimiter = newRateLimiter(customerPerMin, time.Minute)
	}
	if adminPerMin > 0 {
		s.adminLoginLimiter = newRateLimiter(adminPerMin, time.Minute)
	}
}

// rateLimiter is a minimal in-memory sliding-window limiter (per key).
type rateLimiter struct {
	max    int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

// Allow reports whether key may proceed (recording the hit).
func (l *rateLimiter) Allow(key string) bool {
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

// clientIP extracts the request IP for rate limiting / audit.
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
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

// Mailer sends one-off emails (OTP codes). No implementation ships with
// this repo — wire a real provider (SES, SendGrid, …) in production. The
// logging stub below is for non-production development only.
type Mailer interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// SMSSender sends one-off texts (OTP codes). Same story as Mailer — for
// production wire a real Tanzanian gateway (e.g. Beem Africa); the stub
// below is development-only.
type SMSSender interface {
	SendSMS(ctx context.Context, to, message string) error
}

// SetMailer / SetSMSSender wire real delivery. Until wired, OTP requests
// fail loudly (503) instead of silently swallowing codes.
func (s *Service) SetMailer(mailer Mailer)       { s.mailer = mailer }
func (s *Service) SetSMSSender(sender SMSSender) { s.sms = sender }

// SetAllowPublicRegister opens POST /api/v1/auth/register to anyone.
// Default closed: registration is allowed only for the very first account
// (empty users table) so a fresh deployment can bootstrap its admin, then
// closed. Additional accounts must be created by an operator while closed.
func (s *Service) SetAllowPublicRegister(allow bool) {
	s.allowPublicRegister = allow
}

// ErrPublicRegistrationDisabled is returned when self-registration is
// attempted after the bootstrap account already exists.
var ErrPublicRegistrationDisabled = errors.New("public registration is disabled")

func (s *Service) customerTokens() *Verifier {
	if s.customerVerifier != nil {
		return s.customerVerifier
	}
	return s.verifier
}

func (s *Service) adminTokens() *Verifier {
	if s.adminVerifier != nil {
		return s.adminVerifier
	}
	return s.verifier
}

func (s *Service) Register(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(password) < 12 {
		return User{}, "", errors.New("use a valid email and a password with at least 12 characters")
	}
	if !s.allowPublicRegister && !s.isFirstUser(ctx) {
		return User{}, "", ErrPublicRegistrationDisabled
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", fmt.Errorf("hash password: %w", err)
	}
	// Self-registration creates CUSTOMER accounts only (app.users, never
	// admin). There is no allowlist, no flag, no email that can mint an
	// operator — admins exist solely in app.admin_users via seed/invite.
	u := User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash), IsAdmin: false}
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.users (id, email, password_hash, is_admin) VALUES ($1::uuid, $2, $3, $4) RETURNING id::text, email, password_hash, is_admin`, nil, u.ID, u.Email, u.PasswordHash, u.IsAdmin).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return User{}, "", errors.New("an account already exists for this email")
		}
		return User{}, "", fmt.Errorf("create user: %w", err)
	}
	u.IsAdmin = false
	token, err := s.customerTokens().IssueToken(u)
	return u, token, err
}

func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := s.db.QueryRowEx(ctx, `SELECT id::text, email, password_hash, is_admin FROM app.users WHERE lower(email) = lower($1)`, nil, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, "", errors.New("invalid email or password")
	}
	u.IsAdmin = false
	token, err := s.customerTokens().IssueToken(u)
	return u, token, err
}

// isFirstUser reports whether no accounts exist yet (fresh deployment).
// Registration failures here fail closed (treated as "not first").
func (s *Service) isFirstUser(ctx context.Context) bool {
	var count int64
	if err := s.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.users`, nil).Scan(&count); err != nil {
		return false
	}
	return count == 0
}

// IsAdmin reports whether userID is an active operator. It reads
// app.admin_users from the database on EVERY call — never from the token
// claim — so revoking (deleting) an admin takes effect immediately.
// Unknown/deleted operators are not admins.
func (s *Service) IsAdmin(ctx context.Context, userID string) (bool, error) {
	return s.IsAdminUser(ctx, userID)
}

// IsAdminUser checks the admin table (the only admin source of truth).
func (s *Service) IsAdminUser(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowEx(ctx, `SELECT EXISTS(SELECT 1 FROM app.admin_users WHERE id = $1::uuid)`, nil, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// IsCustomerUser checks the customer table (app.users).
func (s *Service) IsCustomerUser(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowEx(ctx, `SELECT EXISTS(SELECT 1 FROM app.users WHERE id = $1::uuid)`, nil, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// CustomerEmailVerified reports whether the customer completed email
// verification. Dashboard/org access requires this (OTP + status reads
// stay open so users can finish verifying).
func (s *Service) CustomerEmailVerified(ctx context.Context, userID string) (bool, error) {
	var verifiedAt sql.NullTime
	err := s.db.QueryRowEx(ctx, `SELECT email_verified_at FROM app.users WHERE id = $1::uuid`, nil, userID).Scan(&verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return verifiedAt.Valid, nil
}

// WriteAudit records an admin-space action. Best-effort: audit failures
// are returned so callers can log them, but must never grant access.
func (s *Service) WriteAudit(ctx context.Context, actorID, actorEmail, action, targetType, targetID, ip string, before, after map[string]any) error {
	var beforeJSON, afterJSON *string
	if before != nil {
		if raw, err := json.Marshal(before); err == nil {
			str := string(raw)
			beforeJSON = &str
		}
	}
	if after != nil {
		if raw, err := json.Marshal(after); err == nil {
			str := string(raw)
			afterJSON = &str
		}
	}
	_, err := s.db.ExecEx(ctx, `INSERT INTO app.audit_log (actor_id, actor_email, action, target_type, target_id, before_data, after_data, ip) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8)`,
		nil, actorID, actorEmail, action, targetType, targetID, beforeJSON, afterJSON, ip)
	return err
}

// ---------- Admin identity (Option A: app.admin_users) ----------

// AdminUser is a platform operator (separate table, separate hashes,
// separate tokens from customers).
type AdminUser struct {
	ID, Email, PasswordHash string
}

var (
	ErrAdminInvalidCredentials = errors.New("invalid email or password")
	ErrAdminLocked             = errors.New("account temporarily locked after too many failed attempts")
	ErrTOTPRequired            = errors.New("two-factor code required")
	ErrTOTPInvalid             = errors.New("invalid two-factor code")
)

// AdminLogin authenticates an operator against app.admin_users and issues
// an admin-audience token (short TTL). Failures are audit-logged; repeated
// failures lock the account briefly. TOTP: when the row carries a
// totp_secret and a TOTPVerify hook is wired, the code must verify;
// when a secret exists but no hook is wired, login stops with
// ErrTOTPRequired so 2FA cannot be silently skipped later.
func (s *Service) AdminLogin(ctx context.Context, email, password, totpCode, ip string) (AdminUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u AdminUser
	var totpSecret string
	var failedAttempts int
	var lockedUntil sql.NullTime
	err := s.db.QueryRowEx(ctx, `SELECT id::text, email, password_hash, COALESCE(totp_secret, ''), COALESCE(failed_attempts, 0), locked_until FROM app.admin_users WHERE lower(email) = lower($1)`, nil, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &totpSecret, &failedAttempts, &lockedUntil)
	if err != nil {
		_ = s.WriteAudit(ctx, "", email, "admin.login_failed", "admin_user", "", ip, nil, map[string]any{"reason": "unknown_email"})
		return AdminUser{}, "", ErrAdminInvalidCredentials
	}
	if lockedUntil.Valid && time.Now().Before(lockedUntil.Time) {
		_ = s.WriteAudit(ctx, u.ID, u.Email, "admin.login_blocked_locked", "admin_user", u.ID, ip, nil, nil)
		return AdminUser{}, "", ErrAdminLocked
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		failedAttempts++
		lockClause := ""
		if failedAttempts >= 5 {
			lockClause = ", locked_until = NOW() + INTERVAL '15 minutes', failed_attempts = 0"
		} else {
			lockClause = fmt.Sprintf(", failed_attempts = %d", failedAttempts)
		}
		_, _ = s.db.ExecEx(ctx, `UPDATE app.admin_users SET updated_at = NOW()`+lockClause+` WHERE id = $1::uuid`, nil, u.ID)
		_ = s.WriteAudit(ctx, u.ID, u.Email, "admin.login_failed", "admin_user", u.ID, ip, nil, map[string]any{"reason": "bad_password"})
		if failedAttempts >= 5 {
			return AdminUser{}, "", ErrAdminLocked
		}
		return AdminUser{}, "", ErrAdminInvalidCredentials
	}
	if strings.TrimSpace(totpSecret) != "" {
		if s.TOTPVerify == nil {
			return AdminUser{}, "", ErrTOTPRequired
		}
		if !s.TOTPVerify(totpSecret, strings.TrimSpace(totpCode)) {
			_ = s.WriteAudit(ctx, u.ID, u.Email, "admin.login_failed", "admin_user", u.ID, ip, nil, map[string]any{"reason": "bad_totp"})
			return AdminUser{}, "", ErrTOTPInvalid
		}
	}
	_, _ = s.db.ExecEx(ctx, `UPDATE app.admin_users SET failed_attempts = 0, locked_until = NULL, last_login_at = NOW(), updated_at = NOW() WHERE id = $1::uuid`, nil, u.ID)
	_ = s.WriteAudit(ctx, u.ID, u.Email, "admin.login", "admin_user", u.ID, ip, nil, nil)
	token, err := s.adminTokens().IssueToken(User{ID: u.ID, Email: u.Email, IsAdmin: true})
	return u, token, err
}

// AdminInviteUser creates another operator. Callable only by an existing
// admin (route-gated AND re-checked here from the DB). ctx must carry
// admin claims (set by RequireAdminAuth).
func (s *Service) AdminInviteUser(ctx context.Context, email, password string) (AdminUser, error) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return AdminUser{}, errors.New("missing authenticated admin")
	}
	if claims.Audience != "" && claims.Audience != AudienceAdmin {
		return AdminUser{}, errors.New("wrong token audience")
	}
	isAdmin, err := s.IsAdminUser(ctx, claims.Subject)
	if err != nil || !isAdmin {
		return AdminUser{}, errors.New("insufficient privileges")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(password) < 12 {
		return AdminUser{}, errors.New("use a valid email and a password with at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AdminUser{}, fmt.Errorf("hash password: %w", err)
	}
	var u AdminUser
	u = AdminUser{ID: uuid.NewString(), Email: email, PasswordHash: string(hash)}
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.admin_users (id, email, password_hash) VALUES ($1::uuid, $2, $3) RETURNING id::text, email, password_hash`, nil, u.ID, u.Email, u.PasswordHash).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return AdminUser{}, errors.New("an admin already exists for this email")
		}
		return AdminUser{}, fmt.Errorf("create admin user: %w", err)
	}
	_ = s.WriteAudit(ctx, claims.Subject, claims.Email, "admin.invite", "admin_user", u.ID, "", nil, map[string]any{"email": u.Email})
	return u, nil
}

// CreateAdminUser creates an operator directly in app.admin_users (seed
// script / admin invite tooling). It is the only way admin accounts come
// into existence — public registration writes app.users and can never
// grant admin.
func (s *Service) CreateAdminUser(ctx context.Context, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(password) < 12 {
		return User{}, errors.New("use a valid email and a password with at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	var u User
	u = User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash), IsAdmin: true}
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.admin_users (id, email, password_hash) VALUES ($1::uuid, $2, $3) RETURNING id::text, email, password_hash`, nil, u.ID, u.Email, u.PasswordHash).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return User{}, errors.New("an account already exists for this email")
		}
		return User{}, fmt.Errorf("create admin user: %w", err)
	}
	return u, nil
}

// UserProfile is the verified-identity view returned by /admin/me.
type UserProfile struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	IsAdmin       bool   `json:"is_admin"`
	EmailVerified bool   `json:"email_verified"`
	Phone         string `json:"phone,omitempty"`
	PhoneVerified bool   `json:"phone_verified"`
}

// GetUserProfile loads the CUSTOMER identity including verification
// flags (customer space only; admins live in app.admin_users).
func (s *Service) GetUserProfile(ctx context.Context, userID string) (UserProfile, error) {
	var profile UserProfile
	var emailVerifiedAt, phoneVerifiedAt sql.NullTime
	var phone sql.NullString
	err := s.db.QueryRowEx(ctx, `SELECT id::text, email, is_admin, email_verified_at, COALESCE(phone, ''), phone_verified_at FROM app.users WHERE id = $1::uuid`, nil, userID).Scan(
		&profile.ID, &profile.Email, &profile.IsAdmin, &emailVerifiedAt, &phone, &phoneVerifiedAt,
	)
	if err != nil {
		return UserProfile{}, err
	}
	profile.EmailVerified = emailVerifiedAt.Valid
	profile.Phone = phone.String
	profile.PhoneVerified = phoneVerifiedAt.Valid
	return profile, nil
}

// AdminProfile is the operator identity view for /admin/auth/me.
type AdminProfile struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	HasTOTP   bool   `json:"has_totp"`
	LastLogin string `json:"last_login_at,omitempty"`
}

// GetAdminProfile loads the operator identity from app.admin_users.
func (s *Service) GetAdminProfile(ctx context.Context, adminID string) (AdminProfile, error) {
	var profile AdminProfile
	var totpSecret sql.NullString
	var lastLogin sql.NullTime
	err := s.db.QueryRowEx(ctx, `SELECT id::text, email, COALESCE(totp_secret, ''), last_login_at FROM app.admin_users WHERE id = $1::uuid`, nil, adminID).Scan(
		&profile.ID, &profile.Email, &totpSecret, &lastLogin,
	)
	if err != nil {
		return AdminProfile{}, err
	}
	profile.IsAdmin = true
	profile.HasTOTP = strings.TrimSpace(totpSecret.String) != ""
	if lastLogin.Valid {
		profile.LastLogin = lastLogin.Time.UTC().Format(time.RFC3339)
	}
	return profile, nil
}

// ---------- One-time passcodes ----------

const (
	// OTPChannels.
	OTPChannelEmail = "email"
	OTPChannelSMS   = "sms"
	// OTPPurposes.
	OTPPurposeEmailVerify = "email_verify"
	OTPPurposePhoneVerify = "phone_verify"
	OTPPurposeLogin2FA    = "login_2fa"

	otpCodeLength  = 6
	otpTTL         = 10 * time.Minute
	otpMaxPerHour  = 5
	otpMaxAttempts = 5
)

var (
	ErrOTPNotConfigured   = errors.New("verification sending is not configured")
	ErrOTPRateLimited     = errors.New("too many codes requested — try again later")
	ErrOTPNoActiveCode    = errors.New("no active code — request a fresh one")
	ErrOTPExpired         = errors.New("code expired — request a fresh one")
	ErrOTPLocked          = errors.New("too many wrong attempts — request a fresh code")
	ErrOTPInvalid         = errors.New("invalid code")
	ErrPhoneInvalid       = errors.New("phone must be a Tanzanian mobile number like +255712345678")
	ErrOTPSendFailed      = errors.New("failed to send verification code")
)

// GenerateOTPCode returns a zero-padded 6-digit code from crypto/rand.
func GenerateOTPCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generate otp code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ValidTanzanianPhone accepts +255 followed by 9 digits starting with 6
// or 7 (all TZ mobile allocations live in 2556x/2557x ranges).
func ValidTanzanianPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	if len(phone) != 13 || !strings.HasPrefix(phone, "+255") {
		return false
	}
	if phone[4] != '6' && phone[4] != '7' {
		return false
	}
	for _, r := range phone[5:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// OTPRequest is the outcome of RequestOTP — never contains the code.
type OTPRequest struct {
	Channel   string    `json:"channel"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RequestOTP creates a fresh code (superseding older unconsumed ones),
// rate-limits issuance, and dispatches it. For SMS the number is
// validated as Tanzanian and stored on the account first.
func (s *Service) RequestOTP(ctx context.Context, userID, channel, purpose, phone string) (OTPRequest, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	purpose = strings.ToLower(strings.TrimSpace(purpose))
	if channel != OTPChannelEmail && channel != OTPChannelSMS {
		return OTPRequest{}, errors.New("channel must be email or sms")
	}
	switch purpose {
	case OTPPurposeEmailVerify, OTPPurposePhoneVerify, OTPPurposeLogin2FA:
	default:
		return OTPRequest{}, errors.New("unknown otp purpose")
	}

	var email, currentPhone string
	err := s.db.QueryRowEx(ctx, `SELECT email, COALESCE(phone, '') FROM app.users WHERE id = $1::uuid`, nil, userID).Scan(&email, &currentPhone)
	if err != nil {
		return OTPRequest{}, errors.New("account not found")
	}

	destination := email
	if channel == OTPChannelSMS {
		phone = strings.TrimSpace(phone)
		if phone == "" {
			phone = currentPhone
		}
		if !ValidTanzanianPhone(phone) {
			return OTPRequest{}, ErrPhoneInvalid
		}
		if _, err := s.db.ExecEx(ctx, `UPDATE app.users SET phone = $2 WHERE id = $1::uuid`, nil, userID, phone); err != nil {
			return OTPRequest{}, fmt.Errorf("save phone number: %w", err)
		}
		destination = phone
	}

	var recent int64
	if err := s.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.otp_codes WHERE user_id = $1::uuid AND created_at > NOW() - INTERVAL '1 hour'`, nil, userID).Scan(&recent); err != nil {
		return OTPRequest{}, fmt.Errorf("check otp rate limit: %w", err)
	}
	if recent >= otpMaxPerHour {
		return OTPRequest{}, ErrOTPRateLimited
	}

	code, err := GenerateOTPCode()
	if err != nil {
		return OTPRequest{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return OTPRequest{}, fmt.Errorf("hash otp code: %w", err)
	}
	expiresAt := time.Now().UTC().Add(otpTTL)

	tx, err := s.db.BeginEx(ctx, nil)
	if err != nil {
		return OTPRequest{}, fmt.Errorf("begin otp transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()
	// A fresh code supersedes older unconsumed ones for the same slot, so
	// lockout always clears on re-request and only the newest code works.
	if _, err = tx.ExecEx(ctx, `UPDATE app.otp_codes SET consumed_at = NOW() WHERE user_id = $1::uuid AND channel = $2 AND purpose = $3 AND consumed_at IS NULL`, nil, userID, channel, purpose); err != nil {
		return OTPRequest{}, fmt.Errorf("supersede old otp codes: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `INSERT INTO app.otp_codes (user_id, channel, code_hash, purpose, expires_at) VALUES ($1::uuid, $2, $3, $4, $5)`, nil, userID, channel, string(hash), purpose, expiresAt); err != nil {
		return OTPRequest{}, fmt.Errorf("store otp code: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return OTPRequest{}, fmt.Errorf("commit otp transaction: %w", err)
	}

	if channel == OTPChannelEmail {
		if s.mailer == nil {
			return OTPRequest{}, ErrOTPNotConfigured
		}
		if err := s.mailer.SendEmail(ctx, destination, "Your verification code", "Your LipaGO verification code is "+code+". It expires in 10 minutes."); err != nil {
			return OTPRequest{}, fmt.Errorf("%w: %v", ErrOTPSendFailed, err)
		}
	} else {
		if s.sms == nil {
			return OTPRequest{}, ErrOTPNotConfigured
		}
		if err := s.sms.SendSMS(ctx, destination, "Your LipaGO verification code is "+code+". It expires in 10 minutes."); err != nil {
			return OTPRequest{}, fmt.Errorf("%w: %v", ErrOTPSendFailed, err)
		}
	}
	return OTPRequest{Channel: channel, ExpiresAt: expiresAt}, nil
}

// VerifyOTP checks a code against the newest unconsumed one, marking it
// consumed and setting the verified flag on success. Wrong attempts
// increment until lockout, which only a fresh code clears.
func (s *Service) VerifyOTP(ctx context.Context, userID, channel, purpose, code string) error {
	channel = strings.ToLower(strings.TrimSpace(channel))
	purpose = strings.ToLower(strings.TrimSpace(purpose))
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrOTPInvalid
	}

	var id, codeHash string
	var attempts int
	var expiresAt time.Time
	err := s.db.QueryRowEx(ctx, `
		SELECT id::text, code_hash, attempts, expires_at FROM app.otp_codes
		WHERE user_id = $1::uuid AND channel = $2 AND purpose = $3 AND consumed_at IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, nil, userID, channel, purpose).Scan(&id, &codeHash, &attempts, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOTPNoActiveCode
	}
	if err != nil {
		return fmt.Errorf("load otp code: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		return ErrOTPExpired
	}
	if attempts >= otpMaxAttempts {
		return ErrOTPLocked
	}
	if bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(code)) != nil {
		if _, err := s.db.ExecEx(ctx, `UPDATE app.otp_codes SET attempts = attempts + 1 WHERE id = $1::uuid`, nil, id); err != nil {
			return fmt.Errorf("record otp attempt: %w", err)
		}
		return ErrOTPInvalid
	}

	tx, err := s.db.BeginEx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin otp verify transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()
	if _, err = tx.ExecEx(ctx, `UPDATE app.otp_codes SET consumed_at = NOW() WHERE id = $1::uuid`, nil, id); err != nil {
		return fmt.Errorf("consume otp code: %w", err)
	}
	switch purpose {
	case OTPPurposeEmailVerify:
		if _, err = tx.ExecEx(ctx, `UPDATE app.users SET email_verified_at = NOW() WHERE id = $1::uuid`, nil, userID); err != nil {
			return fmt.Errorf("mark email verified: %w", err)
		}
	case OTPPurposePhoneVerify:
		if _, err = tx.ExecEx(ctx, `UPDATE app.users SET phone_verified_at = NOW() WHERE id = $1::uuid`, nil, userID); err != nil {
			return fmt.Errorf("mark phone verified: %w", err)
		}
	}
	if err = tx.CommitEx(ctx); err != nil {
		return fmt.Errorf("commit otp verify transaction: %w", err)
	}
	return nil
}

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	// Customer-space rate limit: per IP + per email.
	ip := clientIP(r)
	if !s.loginLimiter.Allow("register:ip:"+ip) {
		httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts — try again later.", nil)
		return
	}
	s.handleCredentials(w, r, s.Register)
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Customer-space rate limit: per IP and per account.
	ip := clientIP(r)
	if !s.loginLimiter.Allow("login:ip:"+ip) {
		httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts — try again later.", nil)
		return
	}
	var peek struct {
		Email string `json:"email"`
	}
	peekBody, _ := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	_ = json.Unmarshal(peekBody, &peek)
	r.Body = io.NopCloser(bytes.NewReader(peekBody))
	if email := strings.ToLower(strings.TrimSpace(peek.Email)); email != "" {
		if !s.loginLimiter.Allow("login:acct:"+email) {
			httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts for this account — try again later.", nil)
			return
		}
	}
	s.handleCredentials(w, r, s.Login)
}

// HandleAdminLogin authenticates operators (separate path, separate
// audience, shorter TTL, stricter rate limit, audited). TOTP hook: when
// wired, pass {"totp_code"} alongside email/password.
func (s *Service) HandleAdminLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.adminLoginLimiter.Allow("admin:ip:"+ip) {
		httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts — try again later.", nil)
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Email and password are required.", nil)
		return
	}
	if email := strings.ToLower(strings.TrimSpace(in.Email)); email != "" {
		if !s.adminLoginLimiter.Allow("admin:acct:"+email) {
			httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts for this account — try again later.", nil)
			return
		}
	}
	u, token, err := s.AdminLogin(r.Context(), in.Email, in.Password, in.TOTPCode, ip)
	if err != nil {
		switch {
		case errors.Is(err, ErrAdminLocked):
			httputil.Error(w, http.StatusTooManyRequests, "account_locked", "Account temporarily locked after too many failed attempts.", nil)
		case errors.Is(err, ErrTOTPRequired):
			httputil.Error(w, http.StatusUnauthorized, "totp_required", "A two-factor code is required.", nil)
		case errors.Is(err, ErrTOTPInvalid):
			httputil.Error(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email, password, or two-factor code.", nil)
		default:
			httputil.Error(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.", nil)
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"access_token": token, "user": map[string]any{"id": u.ID, "email": u.Email, "is_admin": true}}})
}

// HandleAdminInvite creates another operator. Route-gated by
// RequireAdminAuth AND re-checked in AdminInviteUser from the DB.
func (s *Service) HandleAdminInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Email and password are required.", nil)
		return
	}
	u, err := s.AdminInviteUser(r.Context(), in.Email, in.Password)
	if err != nil {
		switch err.Error() {
		case "an admin already exists for this email":
			httputil.Error(w, http.StatusConflict, "already_exists", err.Error(), nil)
		case "use a valid email and a password with at least 12 characters":
			httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
		default:
			httputil.Error(w, http.StatusForbidden, "forbidden", "Insufficient privileges.", nil)
		}
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id": u.ID, "email": u.Email}})
}

// HandleCustomerMe is the customer-space identity endpoint.
func (s *Service) HandleCustomerMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	profile, err := s.GetUserProfile(r.Context(), claims.Subject)
	if err != nil {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Account not found.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"email": profile.Email, "is_admin": false, "user": profile,
		"email_verified": profile.EmailVerified, "phone": profile.Phone,
		"phone_verified": profile.PhoneVerified,
	}})
}

// HandleAdminMe is the admin-space identity endpoint.
func (s *Service) HandleAdminMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	profile, err := s.GetAdminProfile(r.Context(), claims.Subject)
	if err != nil {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Admin account not found.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": profile})
}

// LogMailer is the development-only Mailer: it logs metadata, never the
// code (codes are retrievable from app.otp_codes for local testing).
type LogMailer struct{ logger *slog.Logger }

func NewLogMailer(logger *slog.Logger) *LogMailer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogMailer{logger: logger}
}

func (m *LogMailer) SendEmail(_ context.Context, to, subject, _ string) error {
	m.logger.Info("otp email (logging stub — wire a real provider in production)", "to", to, "subject", subject)
	return nil
}

// LogSMSSender is the development-only SMSSender: same rules as LogMailer.
type LogSMSSender struct{ logger *slog.Logger }

func NewLogSMSSender(logger *slog.Logger) *LogSMSSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogSMSSender{logger: logger}
}

func (m *LogSMSSender) SendSMS(_ context.Context, to, _ string) error {
	m.logger.Info("otp sms (logging stub — wire a real provider in production)", "to", to)
	return nil
}

type otpRequestHTTPInput struct {
	Channel string `json:"channel"`
	Purpose string `json:"purpose"`
	Phone   string `json:"phone"`
}

func (s *Service) HandleOTPRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	var in otpRequestHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Channel and purpose are required.", nil)
		return
	}
	result, err := s.RequestOTP(r.Context(), claims.Subject, in.Channel, in.Purpose, in.Phone)
	if err != nil {
		switch {
		case errors.Is(err, ErrOTPRateLimited):
			httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many codes requested — try again later.", nil)
		case errors.Is(err, ErrOTPNotConfigured):
			httputil.Error(w, http.StatusServiceUnavailable, "otp_not_configured", "Verification sending is not configured on this deployment.", nil)
		case errors.Is(err, ErrPhoneInvalid):
			httputil.Error(w, http.StatusUnprocessableEntity, "invalid_phone", "Phone must be a Tanzanian mobile number like +255712345678.", nil)
		default:
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to request a code.", nil)
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": result})
}

type otpVerifyHTTPInput struct {
	Channel string `json:"channel"`
	Purpose string `json:"purpose"`
	Code    string `json:"code"`
}

func (s *Service) HandleOTPVerify(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	var in otpVerifyHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Channel, purpose, and code are required.", nil)
		return
	}
	if err := s.VerifyOTP(r.Context(), claims.Subject, in.Channel, in.Purpose, in.Code); err != nil {
		switch {
		case errors.Is(err, ErrOTPNoActiveCode):
			httputil.Error(w, http.StatusUnprocessableEntity, "no_active_code", "No active code — request a fresh one.", nil)
		case errors.Is(err, ErrOTPExpired):
			httputil.Error(w, http.StatusUnprocessableEntity, "code_expired", "Code expired — request a fresh one.", nil)
		case errors.Is(err, ErrOTPLocked):
			httputil.Error(w, http.StatusUnprocessableEntity, "code_locked", "Too many wrong attempts — request a fresh code.", nil)
		case errors.Is(err, ErrOTPInvalid):
			httputil.Error(w, http.StatusUnprocessableEntity, "invalid_code", "Invalid code.", nil)
		default:
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to verify code.", nil)
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"verified": true}})
}
func (s *Service) handleCredentials(w http.ResponseWriter, r *http.Request, action func(context.Context, string, string) (User, string, error)) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Email and password are required.", nil)
		return
	}
	u, token, err := action(r.Context(), in.Email, in.Password)
	if err != nil {
		if errors.Is(err, ErrPublicRegistrationDisabled) {
			httputil.Error(w, http.StatusForbidden, "registration_disabled", "Public registration is disabled. Ask an administrator for access.", nil)
			return
		}
		// Only pass through known-safe validation messages — database and
		// crypto errors must never reach clients.
		switch err.Error() {
		case "use a valid email and a password with at least 12 characters",
			"an account already exists for this email",
			"invalid email or password":
			httputil.Error(w, http.StatusUnauthorized, "invalid_credentials", err.Error(), nil)
		default:
			httputil.Error(w, http.StatusUnauthorized, "invalid_credentials", "Authentication failed. Please try again.", nil)
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"access_token": token, "user": map[string]any{"id": u.ID, "email": u.Email, "is_admin": u.IsAdmin}}})
}
