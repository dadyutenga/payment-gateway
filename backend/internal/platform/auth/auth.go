package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/platform/config"
	"azsubay-payments-gateway/internal/shared/httputil"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	Expiry   int64  `json:"exp"`
	IssuedAt int64  `json:"iat"`
}

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
}

func NewVerifier(cfg config.AuthConfig, _ any) *Verifier {
	return &Verifier{secret: []byte(cfg.JWTSecret), tokenTTL: cfg.TokenTTL}
}

func (v *Verifier) IssueToken(user User) (string, error) {
	now := time.Now().Unix()
	claims := Claims{Subject: user.ID, Email: user.Email, IsAdmin: user.IsAdmin, IssuedAt: now, Expiry: now + int64(v.tokenTTL.Seconds())}
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
	return claims, nil
}

type Service struct {
	db                  *pgx.ConnPool
	verifier            *Verifier
	allowPublicRegister bool
	mailer              Mailer
	sms                 SMSSender
}

func NewService(db *pgx.ConnPool, verifier *Verifier) *Service {
	return &Service{db: db, verifier: verifier}
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
	// Self-registration never grants admin — not even to the bootstrap
	// account, and never by matching an env allowlist (removed). Admins
	// are created via the seed script or promoted by an existing admin.
	u := User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash), IsAdmin: false}
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.users (id, email, password_hash, is_admin) VALUES ($1::uuid, $2, $3, $4) RETURNING id::text, email, password_hash, is_admin`, nil, u.ID, u.Email, u.PasswordHash, u.IsAdmin).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return User{}, "", errors.New("an account already exists for this email")
		}
		return User{}, "", fmt.Errorf("create user: %w", err)
	}
	token, err := s.verifier.IssueToken(u)
	return u, token, err
}

func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := s.db.QueryRowEx(ctx, `SELECT id::text, email, password_hash, is_admin FROM app.users WHERE lower(email) = lower($1)`, nil, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, "", errors.New("invalid email or password")
	}
	token, err := s.verifier.IssueToken(u)
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

// IsAdmin reads the current admin flag from the database. Admin-gated
// requests must call this per request instead of trusting the is_admin
// claim baked into the token — otherwise revoking admin has no effect
// until the token (up to AUTH_TOKEN_TTL) expires. Unknown/deleted users
// are not admins.
func (s *Service) IsAdmin(ctx context.Context, userID string) (bool, error) {
	var isAdmin bool
	err := s.db.QueryRowEx(ctx, `SELECT is_admin FROM app.users WHERE id = $1::uuid`, nil, userID).Scan(&isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isAdmin, nil
}

// CreateAdminUser creates an admin directly (seed script / operator
// tooling). It is the only way admin accounts come into existence —
// public registration never grants admin.
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
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.users (id, email, password_hash, is_admin, email_verified_at) VALUES ($1::uuid, $2, $3, $4, NOW()) RETURNING id::text, email, password_hash, is_admin`, nil, u.ID, u.Email, u.PasswordHash, u.IsAdmin).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin)
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

// GetUserProfile loads the caller's identity including verification flags.
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
		if err := s.mailer.SendEmail(ctx, destination, "Your verification code", "Your AZSUBAY verification code is "+code+". It expires in 10 minutes."); err != nil {
			return OTPRequest{}, fmt.Errorf("%w: %v", ErrOTPSendFailed, err)
		}
	} else {
		if s.sms == nil {
			return OTPRequest{}, ErrOTPNotConfigured
		}
		if err := s.sms.SendSMS(ctx, destination, "Your AZSUBAY verification code is "+code+". It expires in 10 minutes."); err != nil {
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
	s.handleCredentials(w, r, s.Register)
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	s.handleCredentials(w, r, s.Login)
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
