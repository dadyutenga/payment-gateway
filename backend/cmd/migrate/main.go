package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"azsubay-payments-gateway/internal/platform/config"

	"github.com/jackc/pgx"
)

type migration struct {
	version  string
	name     string
	upPath   string
	downPath string
}

func main() {
	var (
		action      = flag.String("action", "up", "migration action: up or down")
		dir         = flag.String("dir", "migrations", "migration directory")
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "postgres connection string")
	)
	flag.Parse()
	if strings.TrimSpace(*databaseURL) == "" {
		config.LoadDotEnv(".env")
		*databaseURL = os.Getenv("DATABASE_URL")
	}

	if strings.TrimSpace(*databaseURL) == "" {
		log.Fatal("DATABASE_URL or -database-url is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connConfig, err := pgx.ParseConnectionString(*databaseURL)
	if err != nil {
		log.Fatalf("parse database url: %v", err)
	}

	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:     connConfig,
		MaxConnections: 4,
		AcquireTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if err := ensureMigrationsTable(ctx, pool); err != nil {
		log.Fatalf("ensure schema_migrations table: %v", err)
	}

	migrations, err := loadMigrations(*dir)
	if err != nil {
		log.Fatalf("load migrations: %v", err)
	}

	switch strings.ToLower(strings.TrimSpace(*action)) {
	case "up":
		if err := migrateUp(ctx, pool, migrations); err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
	case "down":
		if err := migrateDown(ctx, pool, migrations); err != nil {
			log.Fatalf("rollback migration: %v", err)
		}
	default:
		log.Fatalf("unsupported action %q", *action)
	}
}

func ensureMigrationsTable(ctx context.Context, pool *pgx.ConnPool) error {
	const query = `
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		);
	`
	_, err := pool.ExecEx(ctx, query, nil)
	return err
}

func loadMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	byVersion := map[string]*migration{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			version := strings.TrimSuffix(name, ".up.sql")
			m := ensureMigration(byVersion, version)
			m.version = version
			m.name = name
			m.upPath = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".down.sql"):
			version := strings.TrimSuffix(name, ".down.sql")
			m := ensureMigration(byVersion, version)
			m.version = version
			m.name = name
			m.downPath = filepath.Join(dir, name)
		}
	}

	migrations := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		migrations = append(migrations, *m)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func ensureMigration(byVersion map[string]*migration, version string) *migration {
	if existing, ok := byVersion[version]; ok {
		return existing
	}
	byVersion[version] = &migration{}
	return byVersion[version]
}

func migrateUp(ctx context.Context, pool *pgx.ConnPool, migrations []migration) error {
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] || m.upPath == "" {
			continue
		}

		sqlBytes, err := os.ReadFile(m.upPath)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.upPath, err)
		}

		tx, err := pool.BeginEx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecEx(ctx, string(sqlBytes), nil); err != nil {
			_ = tx.RollbackEx(ctx)
			return fmt.Errorf("execute migration %s: %w", m.upPath, err)
		}
		if _, err := tx.ExecEx(ctx, `INSERT INTO public.schema_migrations (version) VALUES ($1)`, nil, m.version); err != nil {
			_ = tx.RollbackEx(ctx)
			return err
		}
		if err := tx.CommitEx(ctx); err != nil {
			return err
		}

		log.Printf("applied migration %s", m.version)
	}

	return nil
}

func migrateDown(ctx context.Context, pool *pgx.ConnPool, migrations []migration) error {
	applied, err := latestAppliedVersion(ctx, pool)
	if err != nil {
		return err
	}
	if applied == "" {
		log.Print("no applied migrations to roll back")
		return nil
	}

	var target *migration
	for i := range migrations {
		if migrations[i].version == applied {
			target = &migrations[i]
			break
		}
	}
	if target == nil || target.downPath == "" {
		return fmt.Errorf("no down migration found for %s", applied)
	}

	sqlBytes, err := os.ReadFile(target.downPath)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", target.downPath, err)
	}

	tx, err := pool.BeginEx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecEx(ctx, string(sqlBytes), nil); err != nil {
		_ = tx.RollbackEx(ctx)
		return fmt.Errorf("execute rollback %s: %w", target.downPath, err)
	}
	if _, err := tx.ExecEx(ctx, `DELETE FROM public.schema_migrations WHERE version = $1`, nil, target.version); err != nil {
		_ = tx.RollbackEx(ctx)
		return err
	}
	if err := tx.CommitEx(ctx); err != nil {
		return err
	}

	log.Printf("rolled back migration %s", target.version)
	return nil
}

func appliedVersions(ctx context.Context, pool *pgx.ConnPool) (map[string]bool, error) {
	rows, err := pool.QueryEx(ctx, `SELECT version FROM public.schema_migrations`, nil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out[version] = true
	}
	return out, rows.Err()
}

func latestAppliedVersion(ctx context.Context, pool *pgx.ConnPool) (string, error) {
	var version string
	err := pool.QueryRowEx(ctx, `SELECT version FROM public.schema_migrations ORDER BY version DESC LIMIT 1`, nil).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return version, err
}
