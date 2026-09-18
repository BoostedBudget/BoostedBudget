//go:build integration

package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/disposabledb"
	"github.com/stretchr/testify/require"
)

func revenueCatTestDB(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	cfg, err := disposabledb.ParseURL(url)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	schema := "rc_test_" + uuid.NewString()[:8]
	_, err = pool.Exec(context.Background(), `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	pool.Close()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err = pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); pool.Close() })
	_, err = pool.Exec(context.Background(), `CREATE TABLE users (id uuid PRIMARY KEY, had_trial boolean NOT NULL DEFAULT false,
 subscription_tier text NOT NULL DEFAULT 'premium', subscription_status text NOT NULL DEFAULT 'inactive',
 subscription_ends_at timestamptz, manual_tier_override text, manual_tier_ends_at timestamptz,
 retention_started_at timestamptz, retention_expires_at timestamptz, retention_notice_30d_sent_at timestamptz,
 retention_notice_7d_sent_at timestamptz, plaid_cleanup_pending_at timestamptz, updated_at timestamptz DEFAULT now(), is_deleted boolean DEFAULT false)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000214_revenuecat_authority.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(migration))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(migration))
	require.NoError(t, err, "migration idempotency")
	erasureMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000216_revenuecat_erasure_fence.up.sql"))
	require.NoError(t, err)
	for range 2 {
		_, err = pool.Exec(context.Background(), string(erasureMigration))
		require.NoError(t, err)
	}
	id := uuid.NewString()
	_, err = pool.Exec(context.Background(), `INSERT INTO users(id) VALUES($1)`, id)
	require.NoError(t, err)
	return pool, id
}

func TestRevenueCatRepository_TrialRollbackAndConcurrency(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateTrialTx(ctx, tx, id, now))
	require.NoError(t, tx.Rollback(ctx))
	var had bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT had_trial FROM users WHERE id=$1`, id).Scan(&had))
	require.False(t, had)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Error(e)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if e = repo.CreateTrialTx(ctx, tx, id, now); e == nil {
				e = tx.Commit(ctx)
			}
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	ops, err := repo.ListOperations(ctx, id)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.True(t, now.Add(60*24*time.Hour).Equal(*ops[0].ExpiresAt), "trial expiry is exactly 60*24h, independently of timezone")
	state, err := repo.Read(ctx, id)
	require.NoError(t, err)
	require.Zero(t, state.AccessUpdatedAt)
	require.Equal(t, 1, state.PendingOperations)
}

func TestRevenueCatRepository_InboxAtomicAndDeduplicated(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	sentinel := errors.New("queue down")
	accepted, err := repo.AcceptWebhook(ctx, "evt1", id, "INITIAL_PURCHASE", func(context.Context, pgx.Tx, string) error { return sentinel })
	require.ErrorIs(t, err, sentinel)
	require.False(t, accepted)
	count := 0
	enqueue := func(context.Context, pgx.Tx, string) error { count++; return nil }
	accepted, err = repo.AcceptWebhook(ctx, "evt1", id, "INITIAL_PURCHASE", enqueue)
	require.NoError(t, err)
	require.True(t, accepted)
	accepted, err = repo.AcceptWebhook(ctx, "evt1", id, "INITIAL_PURCHASE", enqueue)
	require.NoError(t, err)
	require.False(t, accepted)
	require.Equal(t, 1, count)
}

func TestRevenueCatRepository_SupersedesOnlySameClassAndRollsBack(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	for _, key := range []string{"comp_premium", "trial_premium", "comp_household_3"} {
		_, source, _, _ := domain.RevenueCatKey(key)
		op := domain.RevenueCatOperation{UserID: id, Class: source, Key: key, Reason: "test", CreatedAt: time.Now()}
		require.NoError(t, repo.RequestOperation(ctx, &op))
	}
	ops, err := repo.ListOperations(ctx, id)
	require.NoError(t, err)
	require.Len(t, ops, 3)
	stages := map[string]string{}
	for _, op := range ops {
		stages[op.Key] = op.Stage
	}
	require.Equal(t, "superseded", stages["comp_premium"])
	require.Equal(t, "pending", stages["trial_premium"])
	require.Equal(t, "pending", stages["comp_household_3"])
	sentinel := errors.New("rollback")
	err = repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		state, e := s.Read(ctx)
		if e != nil {
			return e
		}
		state.AccessUpdatedAt = time.Now()
		if e = s.Save(ctx, state); e != nil {
			return e
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	state, err := repo.Read(ctx, id)
	require.NoError(t, err)
	require.Zero(t, state.AccessUpdatedAt)
}

func TestRevenueCatRepository_BillingEncryptedAndAccessSeparate(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		return s.Save(ctx, domain.RevenueCatState{UserID: id, AccessUpdatedAt: now, Grants: []domain.RevenueCatGrant{{Key: "paid_premium"}}, Billing: []domain.RevenueCatBilling{{ID: "sub_sensitive"}}, BillingUpdatedAt: &now})
	}))
	var raw string
	require.NoError(t, pool.QueryRow(ctx, `SELECT billing FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&raw))
	require.Contains(t, raw, "ENC$")
	state, err := repo.Read(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "sub_sensitive", state.Billing[0].ID)
	require.Equal(t, "paid_premium", state.Grants[0].Key)
}

func TestRevenueCatRepository_RollbackRefusesDataAndGDPRErases(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	op := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "fixture"}
	require.NoError(t, repo.RequestOperation(ctx, &op))
	_, err := repo.AcceptWebhook(ctx, "evt", id, "EXPIRATION", func(context.Context, pgx.Tx, string) error { return nil })
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000214_revenuecat_authority.down.sql"))
	require.NoError(t, err)
	require.Contains(t, string(down), "LOCK TABLE %I IN ACCESS EXCLUSIVE MODE", "refusal must prevent inserts racing the emptiness check")
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "rollback refused")
	_, err = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	require.NoError(t, err)
	for _, table := range []string{"revenuecat_customers", "revenuecat_operations", "revenuecat_webhooks"} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err, "down idempotency")
}

func TestRevenueCatRepository_RepairScheduling(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name     string
		state    domain.RevenueCatState
		min, max time.Duration
	}{
		{"active jittered", domain.RevenueCatState{AccessUpdatedAt: now, Grants: []domain.RevenueCatGrant{{Key: "paid_premium"}}}, 12 * time.Minute, 14 * time.Minute},
		{"inactive daily", domain.RevenueCatState{AccessUpdatedAt: now}, 24 * time.Hour, 24 * time.Hour},
		{"failure backoff not tight loop", domain.RevenueCatState{AccessUpdatedAt: now.Add(-time.Hour), AccessError: "rate_limited"}, time.Minute, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, tc.state) }))
			var due time.Time
			require.NoError(t, pool.QueryRow(ctx, `SELECT next_refresh_at FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&due))
			require.GreaterOrEqual(t, due.Sub(now), tc.min)
			require.LessOrEqual(t, due.Sub(now), tc.max)
		})
	}
}

func TestRevenueCatRepository_UnreadableBillingDoesNotDiscardAccess(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		return s.Save(ctx, domain.RevenueCatState{AccessUpdatedAt: time.Now(), Grants: []domain.RevenueCatGrant{{Key: "paid_premium"}}})
	}))
	_, err := pool.Exec(ctx, `UPDATE revenuecat_customers SET billing='ENC$invalid' WHERE user_id=$1`, id)
	require.NoError(t, err)
	state, err := repo.Read(ctx, id)
	require.NoError(t, err)
	a, err := state.ResolveAccess(time.Now())
	require.NoError(t, err)
	require.Equal(t, "paid", a.Source)
	require.Equal(t, "billing_projection_unreadable", state.BillingError)
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
	var raw string
	require.NoError(t, pool.QueryRow(ctx, `SELECT billing FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&raw))
	require.Equal(t, "ENC$invalid", raw, "access-only save must retain unavailable billing bytes")
}

func TestRevenueCatRepository_PendingOperationsFairToNewIntent(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	ctx := context.Background()
	old := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "old", CreatedAt: time.Now().Add(-time.Hour)}
	next := domain.RevenueCatOperation{UserID: id, Class: "trial", Key: "trial_premium", Reason: "new", CreatedAt: time.Now()}
	require.NoError(t, repo.RequestOperation(ctx, &old))
	require.NoError(t, repo.RequestOperation(ctx, &next))
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		return s.FinishOperation(ctx, old.ID, "pending", "provider_unavailable", old.Class)
	}))
	pending, err := repo.PendingOperations(ctx, 1)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, next.ID, pending[0].ID, "permanent older failures must not starve new intents")
}
