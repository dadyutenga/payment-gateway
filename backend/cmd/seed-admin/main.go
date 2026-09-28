// Command seed-admin creates an administrator account in app.admin_users.
//
// Option A identity: operators live ONLY in app.admin_users with their own
// password hashes and admin-audience tokens. Self-registration writes
// app.users (customers) and can never grant admin — this script (or an
// existing admin inviting another admin via POST /api/v1/admin/auth/invite)
// is the only way operators come into existence.
//
// Run it against the same DATABASE_URL as the API:
//
//	go run ./cmd/seed-admin -email admin@example.com -password '...'
//
// Password is optional; when omitted a random one is printed once.
// Existing operators are left alone. A customer row with the same email
// (if any) is left untouched — the two spaces are separate identities.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"lipago/internal/platform/config"
	"lipago/internal/platform/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	var (
		email     = flag.String("email", "", "admin email (required)")
		password  = flag.String("password", "", "admin password (min 12 chars); omit to generate one")
		dbURLFlag = flag.String("database-url", os.Getenv("DATABASE_URL"), "postgres connection string")
	)
	flag.Parse()
	if strings.TrimSpace(*dbURLFlag) == "" {
		config.LoadDotEnv(".env")
		*dbURLFlag = os.Getenv("DATABASE_URL")
	}
	if strings.TrimSpace(*email) == "" {
		log.Fatal("-email is required")
	}
	if strings.TrimSpace(*dbURLFlag) == "" {
		log.Fatal("DATABASE_URL or -database-url is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, config.DatabaseConfig{URL: *dbURLFlag})
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	// The seed must work before migration 000044 is applied too, so make
	// sure the operator table exists (idempotent on migrated databases).
	ensureAdminUsersTable(ctx, db)

	emailNorm := strings.ToLower(strings.TrimSpace(*email))
	if !strings.Contains(emailNorm, "@") {
		log.Fatal("-email must be a valid address")
	}

	pw := strings.TrimSpace(*password)
	generated := false
	if pw == "" {
		pw, err = randomPassword(16)
		if err != nil {
			log.Fatalf("generate password: %v", err)
		}
		generated = true
	}
	if len(pw) < 12 {
		log.Fatal("password must be at least 12 characters")
	}

	var existingID string
	err = db.QueryRowEx(ctx, `SELECT id::text FROM app.admin_users WHERE lower(email) = lower($1)`, nil, emailNorm).Scan(&existingID)
	if err == nil {
		fmt.Printf("user %s is already an admin\n", emailNorm)
		return
	}
	if err != nil && !strings.Contains(fmt.Sprint(err), "no rows") {
		log.Fatalf("lookup admin: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	if _, err := db.ExecEx(ctx, `
		INSERT INTO app.admin_users (id, email, password_hash)
		VALUES ($1::uuid, $2, $3)
	`, nil, uuid.NewString(), emailNorm, string(hash)); err != nil {
		log.Fatalf("create admin user: %v", err)
	}
	fmt.Printf("created admin %s\n", emailNorm)
	if generated {
		fmt.Printf("generated password (shown once): %s\n", pw)
	}
}

// ensureAdminUsersTable creates the operator table when the seed runs
// against a database that predates migration 000044.
func ensureAdminUsersTable(ctx context.Context, db *pgx.ConnPool) {
	const ddl = `
		CREATE TABLE IF NOT EXISTS app.admin_users (
		  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		  email TEXT NOT NULL,
		  password_hash TEXT NOT NULL,
		  totp_secret TEXT NOT NULL DEFAULT '',
		  failed_attempts INTEGER NOT NULL DEFAULT 0,
		  locked_until TIMESTAMPTZ,
		  last_login_at TIMESTAMPTZ,
		  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE UNIQUE INDEX IF NOT EXISTS admin_users_email_lower_idx
		  ON app.admin_users (lower(email));
		ALTER TABLE app.admin_users ADD COLUMN IF NOT EXISTS totp_secret TEXT NOT NULL DEFAULT '';
		ALTER TABLE app.admin_users ADD COLUMN IF NOT EXISTS failed_attempts INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE app.admin_users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
		ALTER TABLE app.admin_users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
	`
	// Best-effort: migration 000044 is the source of truth; this only
	// helps fresh pre-migration databases. Log and continue on failure —
	// the INSERT below will surface real problems.
	if _, err := db.ExecEx(ctx, ddl, nil); err != nil {
		log.Printf("ensure admin_users table: %v", err)
	}
}

func randomPassword(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	out := make([]byte, n)
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
