//go:build integration

package repository

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRevenueCatCompatibilityProjectionRetentionEpisodes(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status='active',manual_tier_override='household_5' WHERE id=$1`, id)
	require.NoError(t, err)
	state := domain.RevenueCatState{UserID: id, AccessUpdatedAt: time.Now()}
	save := func() {
		require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
	}
	save()
	var status, tier string
	var marker, start, end *time.Time
	var override *string
	read := func() {
		require.NoError(t, pool.QueryRow(ctx, `SELECT subscription_status,subscription_tier,plaid_cleanup_pending_at,retention_started_at,retention_expires_at,manual_tier_override FROM users WHERE id=$1`, id).Scan(&status, &tier, &marker, &start, &end, &override))
	}
	read()
	require.Equal(t, "inactive", status)
	require.NotNil(t, marker)
	require.NotNil(t, start)
	require.NotNil(t, end)
	require.Nil(t, override)
	firstMarker, firstStart := *marker, *start
	save()
	read()
	require.Equal(t, firstMarker, *marker)
	require.Equal(t, firstStart, *start)
	state.Grants = []domain.RevenueCatGrant{{Key: "comp_household_3"}}
	save()
	read()
	require.Equal(t, "active", status)
	require.Equal(t, "household_3", tier)
	require.Nil(t, marker)
	require.Nil(t, start)
	state.Grants = nil
	state.AccessUpdatedAt = time.Now().Add(-time.Hour)
	save()
	read()
	require.Equal(t, "active", status)
	require.Nil(t, start)
	state.AccessUpdatedAt = time.Now()
	op := &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "support"}
	require.NoError(t, repo.RequestOperation(ctx, op))
	save()
	read()
	require.Equal(t, "active", status)
	require.Nil(t, start)
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.FinishOperation(ctx, op.ID, "applied", "", op.Class) }))
	due, err := repo.ListDue(ctx, time.Now().Add(time.Second), 100)
	require.NoError(t, err)
	require.Contains(t, due, id)
	save()
	read()
	require.Equal(t, "inactive", status)
	require.NotNil(t, start)

	// --- Extension (todo 3): the two Save behaviours the service-layer drop-guard depends on ---
	//
	// (a) next_refresh_at is pinned to the earliest grant expiry, so RevenueCatRepairWorker wakes
	// the moment a grant lapses rather than at the generic cadence.
	soon := time.Now().Add(2 * time.Minute).UTC().Truncate(time.Millisecond)
	state.Grants = []domain.RevenueCatGrant{{Key: "paid_premium", ExpiresAt: &soon}}
	state.AccessUpdatedAt = time.Now()
	save()
	read()
	require.Equal(t, "active", status)
	require.Nil(t, marker)
	var nextRefresh time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT next_refresh_at FROM revenuecat_customers WHERE user_id=$1`, id).Scan(&nextRefresh))
	require.True(t, nextRefresh.Equal(soon), "next_refresh_at must be pinned to the grant's ExpiresAt: got %v want %v", nextRefresh, soon)

	// (b) Failure path (ii) at the repository boundary: Save only projects a state that still
	// resolves. A state whose ONLY grant has lapsed early-returns without touching the users
	// projection, so the inactive projection is never persisted from here — which is exactly why
	// the fix lives at the fresh-fetch boundary (applyRevenueCatGrants drops the expired grant
	// before Save sees it), not inside ResolveAccess.
	expiredEnd := time.Now().Add(-time.Second)
	state.Grants = []domain.RevenueCatGrant{{Key: "paid_premium", ExpiresAt: &expiredEnd}}
	state.AccessUpdatedAt = time.Now()
	save()
	read()
	require.Equal(t, "active", status, "an expired-only state does not resolve, so Save deliberately does not project it")
	require.Nil(t, marker)
	require.Nil(t, start)

	// The post-drop state the service now hands Save: same user, unusable grant removed.
	state.Grants = nil
	state.AccessUpdatedAt = time.Now()
	save()
	read()
	require.Equal(t, "inactive", status)
	require.NotNil(t, marker)
	require.NotNil(t, start)
}
