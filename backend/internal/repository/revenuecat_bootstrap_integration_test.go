//go:build integration

package repository

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestRevenueCatBootstrapApplyIsConcurrentIdempotentAndPreservesExpiry(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `ALTER TABLE users ADD COLUMN clerk_user_id text, ADD COLUMN trial_ends_at timestamptz, ADD COLUMN paddle_subscription_id text, ADD COLUMN paddle_customer_id text, ADD COLUMN deletion_requested_at timestamptz`)
	require.NoError(t, err)
	expiry := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	enc := newTestFieldEncryptor(t)
	clerk, err := enc.EncryptString("clerk-bootstrap")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE users SET clerk_user_id=$3,trial_ends_at=$2,manual_tier_override='household_3' WHERE id=$1`, id, expiry, clerk)
	require.NoError(t, err)
	repo := NewRevenueCatBootstrapRepository(pool, enc)
	users, err := repo.Inventory(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, "clerk-bootstrap", users[0].ClerkUserID)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := repo.Apply(ctx, id)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	ops, err := NewRevenueCatRepository(pool, nil).ListOperations(ctx, id)
	require.NoError(t, err)
	require.Len(t, ops, 2)
	for _, op := range ops {
		if op.Class == "trial" {
			require.True(t, expiry.Equal(*op.ExpiresAt))
		}
	}
	var had bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT had_trial FROM users WHERE id=$1`, id).Scan(&had))
	require.True(t, had)
}

func TestRevenueCatBootstrapDoesNotOverwriteModernOperation(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `ALTER TABLE users ADD COLUMN clerk_user_id text, ADD COLUMN trial_ends_at timestamptz, ADD COLUMN paddle_subscription_id text, ADD COLUMN paddle_customer_id text, ADD COLUMN deletion_requested_at timestamptz`)
	require.NoError(t, err)
	enc := newTestFieldEncryptor(t)
	clerk, err := enc.EncryptString("clerk-modern")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE users SET clerk_user_id=$2, manual_tier_override='household_5' WHERE id=$1`, id, clerk)
	require.NoError(t, err)
	stateRepo := NewRevenueCatRepository(pool, enc)
	op := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Revoke: true, Reason: "Modern revocation"}
	require.NoError(t, stateRepo.RequestOperation(ctx, &op))
	queued, err := NewRevenueCatBootstrapRepository(pool, enc).Apply(ctx, id)
	require.NoError(t, err)
	require.Zero(t, queued)
	ops, err := stateRepo.ListOperations(ctx, id)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, op.ID, ops[0].ID)
	require.True(t, ops[0].Revoke)
	require.Equal(t, "pending", ops[0].Stage)
}
