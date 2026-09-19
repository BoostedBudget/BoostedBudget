//go:build integration

package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func revenueCatRetentionFixture(t *testing.T) (*pgxpool.Pool, string) {
	pool, id := revenueCatTestDB(t)
	_, err := pool.Exec(context.Background(), `CREATE TABLE households(id uuid PRIMARY KEY,owner_user_id uuid);
 CREATE TABLE household_members(household_id uuid,user_id uuid,status text);
 CREATE TABLE subscription_entitlements(id uuid,user_id uuid,source text,tier text,status text,payer_user_id uuid,household_id uuid,effective_from timestamptz,effective_until timestamptz,created_at timestamptz,updated_at timestamptz);`)
	require.NoError(t, err)
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	require.NoError(t, repo.WithCustomer(context.Background(), id, func(s RevenueCatSession) error {
		return s.Save(context.Background(), domain.RevenueCatState{AccessUpdatedAt: time.Now()})
	}))
	return pool, id
}

func TestRevenueCatRetentionCascadeGuardFencesConcurrentGrant(t *testing.T) {
	pool, id := revenueCatRetentionFixture(t)
	ctx := context.Background()
	profile := NewProfileRepository(pool, newTestFieldEncryptor(t))
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	allowed, err := profile.lockRevenueCatRetentionEligibility(ctx, tx, id)
	require.NoError(t, err)
	require.True(t, allowed)
	// A grant cannot commit while the final deletion decision's locks are held.
	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err = repo.RequestOperation(blockedCtx, &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "support"})
	require.Error(t, err)
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, repo.RequestOperation(ctx, &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "support"}))
	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	allowed, err = profile.lockRevenueCatRetentionEligibility(ctx, tx, id)
	require.NoError(t, err)
	require.False(t, allowed, "new intent committed after external cleanup must prevent cascade")
}
