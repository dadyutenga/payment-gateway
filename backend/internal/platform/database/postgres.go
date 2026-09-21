package database

import (
	"context"
	"fmt"

	"azsubay-payments-gateway/internal/platform/config"
	"github.com/jackc/pgx"
)

// NewPool creates the shared Postgres pool used by repositories.
func NewPool(ctx context.Context, cfg config.DatabaseConfig) (*pgx.ConnPool, error) {
	connConfig, err := pgx.ParseConnectionString(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	poolConfig := pgx.ConnPoolConfig{
		ConnConfig:     connConfig,
		MaxConnections: int(cfg.MaxOpenConns),
		AcquireTimeout: cfg.HealthTimeout,
	}

	pool, err := pgx.NewConnPool(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	healthCtx, cancel := context.WithTimeout(ctx, cfg.HealthTimeout)
	defer cancel()

	var ping int
	if err := pool.QueryRowEx(healthCtx, "SELECT 1", nil).Scan(&ping); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
