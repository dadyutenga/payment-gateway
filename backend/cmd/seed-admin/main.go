// Command seed-admin creates (or promotes) an administrator account.
//
// Self-registration never grants admin — this script is the only automated
// way to mint one. Run it against the same DATABASE_URL as the API:
//
//	go run ./cmd/seed-admin -email admin@example.com -password '...'
//
// Password is optional; when omitted a random one is printed once.
// Existing non-admin accounts are promoted; existing admins are left alone.
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

	"azsubay-payments-gateway/internal/platform/config"
	"azsubay-payments-gateway/internal/platform/database"

	"github.com/google/uuid"
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

	// Promote an existing account if present.
	var existingID, existingIsAdmin string
	err = db.QueryRowEx(ctx, `SELECT id::text, is_admin::text FROM app.users WHERE lower(email) = lower($1)`, nil, emailNorm).Scan(&existingID, &existingIsAdmin)
	switch {
	case err == nil:
		if existingIsAdmin == "t" || existingIsAdmin == "true" {
			fmt.Printf("user %s is already an admin\n", emailNorm)
			return
		}
		if _, err := db.ExecEx(ctx, `UPDATE app.users SET is_admin = true, email_verified_at = COALESCE(email_verified_at, NOW()) WHERE id = $1::uuid`, nil, existingID); err != nil {
			log.Fatalf("promote user: %v", err)
		}
		fmt.Printf("promoted %s to admin\n", emailNorm)
		if generated {
			fmt.Printf("generated password (shown once): %s\n", pw)
		}
		return
	case strings.Contains(err.Error(), "no rows"):
		// fall through to create
	default:
		// pgx returns pgx.ErrNoRows; string check above is best-effort
		if !strings.Contains(fmt.Sprint(err), "no rows") {
			log.Fatalf("lookup user: %v", err)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	// Reuse auth.Service's insert shape without pulling its first-user
	// registration gate (seed must work with users already present).
	if _, err := db.ExecEx(ctx, `
		INSERT INTO app.users (id, email, password_hash, is_admin, email_verified_at)
		VALUES ($1::uuid, $2, $3, true, NOW())
	`, nil, uuid.NewString(), emailNorm, string(hash)); err != nil {
		log.Fatalf("create admin user: %v", err)
	}
	fmt.Printf("created admin %s\n", emailNorm)
	if generated {
		fmt.Printf("generated password (shown once): %s\n", pw)
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
