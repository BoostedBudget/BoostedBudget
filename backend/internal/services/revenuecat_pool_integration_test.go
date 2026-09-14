//go:build integration

package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/disposabledb"
	"github.com/stretchr/testify/require"
)

type rcPoolProfile struct{ pool *pgxpool.Pool }

func (p rcPoolProfile) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{ID: id}
	err := p.pool.QueryRow(ctx, `SELECT clerk_user_id FROM users WHERE id=$1`, id).Scan(&u.ClerkUserID)
	return u, err
}

type rcPoolCipher struct{}

func (rcPoolCipher) Encrypt(s string) (string, error) { return s, nil }
func (rcPoolCipher) Decrypt(s string) (string, error) { return s, nil }

func TestRevenueCatService_OneConnectionPoolDoesNotDeadlock(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	cfg, err := disposabledb.ParseURL(url)
	require.NoError(t, err)
	cfg.MaxConns = 1
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	schema := "rc_pool_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	pool.Close()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); pool.Close() })
	_, err = pool.Exec(ctx, `CREATE TABLE users(id uuid PRIMARY KEY,had_trial boolean NOT NULL DEFAULT false,clerk_user_id text NOT NULL,
 subscription_tier text NOT NULL DEFAULT 'premium', subscription_status text NOT NULL DEFAULT 'inactive',
 subscription_ends_at timestamptz, manual_tier_override text, manual_tier_ends_at timestamptz,
 retention_started_at timestamptz, retention_expires_at timestamptz, retention_notice_30d_sent_at timestamptz,
 retention_notice_7d_sent_at timestamptz, plaid_cleanup_pending_at timestamptz, updated_at timestamptz DEFAULT now(), is_deleted boolean DEFAULT false)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000208_revenuecat_authority.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)
	erasureMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000210_revenuecat_erasure_fence.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(erasureMigration))
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO users(id,clerk_user_id) VALUES($1,'clerk_pool_user')`, id)
	require.NoError(t, err)
	enc, err := appcrypto.NewFieldEncryptor(rcPoolCipher{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := repository.NewRevenueCatRepository(pool, enc)
	p := &rcTestProvider{}
	s := NewRevenueCatService(repo, p, rcPoolProfile{pool})
	t.Run("refresh", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, err := s.Refresh(bounded, id)
		require.NoError(t, err, "profile read must not wait on its own customer transaction")
	})
	t.Run("grant", func(t *testing.T) {
		op := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "pool fixture"}
		require.NoError(t, repo.RequestOperation(ctx, &op))
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		require.NoError(t, s.ProcessOperation(bounded, id, op.ID), "grant identity lookup must precede customer transaction")
	})
	t.Run("persistent identity failures yield to new work", func(t *testing.T) {
		old := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "old identity failure", CreatedAt: time.Now().Add(-time.Hour)}
		next := domain.RevenueCatOperation{UserID: id, Class: "trial", Key: "trial_premium", Reason: "new intent"}
		require.NoError(t, repo.RequestOperation(ctx, &old))
		require.NoError(t, repo.RequestOperation(ctx, &next))
		failing := NewRevenueCatService(repo, p, rcUnavailableProfile{})
		for range 2 {
			require.Error(t, failing.ProcessOperation(ctx, id, old.ID))
		}
		pending, err := repo.PendingOperations(ctx, 1)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, next.ID, pending[0].ID)
		var attempts int
		var message string
		require.NoError(t, pool.QueryRow(ctx, `SELECT attempts,last_error FROM revenuecat_operations WHERE id=$1`, old.ID).Scan(&attempts, &message))
		require.Equal(t, 2, attempts)
		require.Equal(t, "provider_unavailable", message)
	})
}
