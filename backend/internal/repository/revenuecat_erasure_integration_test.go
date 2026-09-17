//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatErasureRollbackCannotDiscardFence(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	down, err := os.ReadFile("../../migrations/000216_revenuecat_erasure_fence.down.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO revenuecat_customers(user_id,erasure_started_at) VALUES($1,now())`, id)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "rollback refused")
	var fenced bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT erasure_started_at IS NOT NULL FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&fenced))
	require.True(t, fenced)
}

func TestRevenueCatErasureRollbackIsIdempotentWhenEmpty(t *testing.T) {
	pool, _ := revenueCatTestDB(t)
	down, err := os.ReadFile("../../migrations/000216_revenuecat_erasure_fence.down.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = pool.Exec(context.Background(), string(down))
		require.NoError(t, err)
	}
}

func TestRevenueCatErasureDurablyFencesNewIntents(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	erasure := NewRevenueCatErasureRepository(pool, newTestFieldEncryptor(t))
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		return s.Save(ctx, domain.RevenueCatState{UserID: id, AccessUpdatedAt: time.Now()})
	}))
	require.NoError(t, erasure.Begin(ctx, id, true))
	state, err := repo.Read(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, state.ErasureStartedAt)
	err = repo.RequestOperation(ctx, &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "must refuse"})
	require.ErrorIs(t, err, domain.ErrRevenueCatErasureStarted)
	require.NoError(t, erasure.Begin(ctx, id, true))
	require.NoError(t, erasure.Confirm(ctx, id))
	state, err = repo.Read(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, state.ErasureConfirmedAt)
	_, err = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&remaining))
	require.Zero(t, remaining, "the final user cascade removes the durable personal erasure intent")
}

func TestRevenueCatErasureRetentionRefusesActiveOrUnconfirmedAccess(t *testing.T) {
	for _, condition := range []string{"active", "pending", "stale"} {
		t.Run(condition, func(t *testing.T) {
			pool, id := revenueCatTestDB(t)
			ctx := context.Background()
			repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
			state := domain.RevenueCatState{AccessUpdatedAt: time.Now()}
			if condition == "active" {
				state.Grants = []domain.RevenueCatGrant{{Key: "comp_premium"}}
			}
			if condition == "stale" {
				state.AccessUpdatedAt = time.Now().Add(-time.Hour)
			}
			require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
			if condition == "pending" {
				require.NoError(t, repo.RequestOperation(ctx, &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "pending"}))
			}
			erasure := NewRevenueCatErasureRepository(pool, newTestFieldEncryptor(t))
			require.Error(t, erasure.Begin(ctx, id, true))
			got, err := repo.Read(ctx, id)
			require.NoError(t, err)
			require.Nil(t, got.ErasureStartedAt)
			require.NoError(t, erasure.Begin(ctx, id, false), "explicit erasure may supersede live access and pending grants")
			got, err = repo.Read(ctx, id)
			require.NoError(t, err)
			require.NotNil(t, got.ErasureStartedAt)
			require.Zero(t, got.PendingOperations)
		})
	}
}
