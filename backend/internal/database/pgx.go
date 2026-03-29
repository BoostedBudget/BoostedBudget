// Package database provides database connection utilities for BoostedBudget.
// It uses pgx v5 for PostgreSQL connections, which is required by River job queue.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds database connection configuration
type Config struct {
	// DatabaseURL is the PostgreSQL connection string
	DatabaseURL string

	// MaxConns is the maximum number of connections in the pool (default: 25)
	MaxConns int32

	// MinConns is the minimum number of connections in the pool (default: 5)
	MinConns int32

	// MaxConnLifetime is the maximum lifetime of a connection (default: 1 hour)
	MaxConnLifetime time.Duration

	// MaxConnIdleTime is the maximum time a connection can be idle (default: 30 minutes)
	MaxConnIdleTime time.Duration

	// HealthCheckPeriod is the interval between health checks (default: 1 minute)
	HealthCheckPeriod time.Duration
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig(databaseURL string) Config {
	return Config{
		DatabaseURL:       databaseURL,
		MaxConns:          25,
		MinConns:          5,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}

// NewPool creates a new pgx connection pool with the given configuration.
// It returns an error if the connection cannot be established.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Apply configuration
	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolConfig.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	// Create the pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// NewPoolFromURL creates a new pgx connection pool with default configuration.
// This is a convenience function for simple use cases.
func NewPoolFromURL(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return NewPool(ctx, DefaultConfig(databaseURL))
}
