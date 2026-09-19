//go:build integration

package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRevenueCatCheckoutConcurrencyAndReleaseOwnership(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	migration, err := os.ReadFile("../../migrations/000217_revenuecat_checkout_intents.up.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = pool.Exec(ctx, string(migration))
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO revenuecat_customers(user_id,access_updated_at,billing_updated_at) VALUES($1,clock_timestamp(),clock_timestamp())`, id)
	require.NoError(t, err)
	repo := NewRevenueCatCheckoutRepository(pool, nil)
	var wg sync.WaitGroup
	intents := make(chan domain.RevenueCatCheckoutIntent, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); intent, e := repo.Begin(ctx, id, "premium_monthly"); intents <- intent; errs <- e }()
	}
	wg.Wait()
	close(intents)
	close(errs)
	successes, blocked := 0, 0
	for e := range errs {
		if e == nil {
			successes++
		} else if errors.Is(e, domain.ErrRevenueCatCheckoutInProgress) {
			blocked++
		} else {
			require.NoError(t, e)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, blocked)
	var intent domain.RevenueCatCheckoutIntent
	for i := range intents {
		if i.ID != "" {
			intent = i
		}
	}
	require.WithinDuration(t, time.Now().Add(30*time.Minute), intent.ExpiresAt, time.Minute)
	require.Equal(t, "premium_monthly", intent.Package, "the derived package must round-trip from real Postgres")
	require.NoError(t, repo.Release(ctx, uuid.NewString(), intent.ID))
	_, err = repo.Begin(ctx, id, "premium_monthly")
	require.ErrorIs(t, err, domain.ErrRevenueCatCheckoutInProgress, "another account cannot release the lease")
	require.NoError(t, repo.Release(ctx, id, intent.ID))
	_, err = repo.Begin(ctx, id, "premium_monthly")
	require.NoError(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM revenuecat_checkout_intents WHERE user_id=$1`, id).Scan(&count))
	require.Equal(t, 2, count, "release preserves operational history")
}

func TestRevenueCatCheckoutRollbackIsIdempotentWhenEmpty(t *testing.T) {
	pool, _ := revenueCatTestDB(t)
	ctx := context.Background()
	up, err := os.ReadFile("../../migrations/000217_revenuecat_checkout_intents.up.sql")
	require.NoError(t, err)
	down, err := os.ReadFile("../../migrations/000217_revenuecat_checkout_intents.down.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(up))
	require.NoError(t, err)
	for range 2 {
		_, err = pool.Exec(ctx, string(down))
		require.NoError(t, err)
	}
}
