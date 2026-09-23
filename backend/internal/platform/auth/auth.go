package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	db              *pgx.ConnPool
	verifier        *Verifier
	bootstrapAdmins map[string]bool
}

func NewService(db *pgx.ConnPool, verifier *Verifier, adminEmails []string) *Service {
	admins := map[string]bool{}
	for _, email := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(email))] = true
	}
	return &Service{db: db, verifier: verifier, bootstrapAdmins: admins}
}

func (s *Service) Register(ctx context.Context, email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(password) < 12 {
		return User{}, "", errors.New("use a valid email and a password with at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", fmt.Errorf("hash password: %w", err)
	}
	u := User{ID: uuid.NewString(), Email: email, PasswordHash: string(hash), IsAdmin: s.bootstrapAdmins[email]}
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

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	s.handleCredentials(w, r, s.Register)
}
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	s.handleCredentials(w, r, s.Login)
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
