//go:build integration

package repository

// #1652 todo 4, Part A — the comp-expiry proof.
//
// The RC comp path is the ONLY comp mechanism left after centralization: the admin "grant comp"
// handler delegates to RequestRevenueCatGrant (admin_subscriptions.go), which records the intent in
// revenuecat_operations and never touches the legacy users.manual_tier_override column. Access is
// then projected by the SAME revenueCatSession.Save path every other grant uses
// (revenuecat_repository.go:163-185), which:
//
//   - clears manual_tier_override/manual_tier_ends_at on every projection (:178), and
//   - arms the retention episode (retention_started_at + plaid_cleanup_pending_at) when the
//     projection resolves inactive (:179-183).
//
// This test drives that path with a comp_* grant and then with the grant dropped off the fresh
// provider read, proving the comp-expiry path arms retention and clears the legacy override.
//
// FALSIFICATION: every assertion below is derived from the RC grant state, never from the legacy
// column. Bypassing RC resolution (projecting from manual_tier_override, or skipping
// ResolveAccess) leaves the override set and the status active, so the test fails.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test -tags integration -p 1 ./internal/repository/... \
//	  -run 'RevenueCat.*Comp' -v

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatCompExpiry_ArmsRetentionAndClearsOverride(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))

	// A pre-RC user: active, with a legacy manual override still set. This is the state the
	// backfill migration (000211) repairs; here it is the "prior" state the RC projection must
	// overwrite.
	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status='active', subscription_tier='household_5',
	 manual_tier_override='household_5', manual_tier_ends_at=clock_timestamp()+interval '30 days' WHERE id=$1`, id)
	require.NoError(t, err)

	// (1) The admin comp path writes revenuecat_operations, NOT the legacy local column.
	op := &domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_household_3", Reason: "support"}
	require.NoError(t, repo.RequestOperation(ctx, op))
	var opCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM revenuecat_operations WHERE user_id=$1 AND class='comp' AND entitlement_key='comp_household_3'`, id).Scan(&opCount))
	require.Equal(t, 1, opCount, "the admin comp path must record the intent in revenuecat_operations")

	var override *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT manual_tier_override FROM users WHERE id=$1`, id).Scan(&override))
	require.NotNil(t, override, "the admin comp path must NOT write the legacy local column")
	require.Equal(t, "household_5", *override)

	// The intent is confirmed by the provider; finish it so no pending operation blocks the
	// inactive projection below (Save's guard: an inactive projection is withheld while an intent
	// is pending).
	require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error {
		return s.FinishOperation(ctx, op.ID, "applied", "")
	}))

	state := domain.RevenueCatState{UserID: id, AccessUpdatedAt: time.Now(), Grants: []domain.RevenueCatGrant{{Key: "comp_household_3"}}}
	save := func() {
		require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
	}
	var status, tier string
	var marker, start *time.Time
	read := func() {
		require.NoError(t, pool.QueryRow(ctx, `SELECT subscription_status,subscription_tier,plaid_cleanup_pending_at,retention_started_at,manual_tier_override FROM users WHERE id=$1`, id).Scan(&status, &tier, &marker, &start, &override))
	}

	// (2) RC confirms the comp grant: Save projects it active AND clears the legacy override.
	save()
	read()
	require.Equal(t, "active", status)
	require.Equal(t, "household_3", tier)
	require.Nil(t, override, "a live comp grant must clear the legacy manual_tier_override")
	require.Nil(t, marker)
	require.Nil(t, start)

	// (3) The comp grant drops off the fresh provider read (it lapsed): the SAME Save path arms
	// retention and projects inactive.
	state.Grants = nil
	state.AccessUpdatedAt = time.Now()
	save()
	read()
	require.Equal(t, "inactive", status, "a dropped comp grant must project inactive")
	require.NotNil(t, marker, "comp expiry must arm the Plaid-cleanup marker")
	require.NotNil(t, start, "comp expiry must start the retention episode")
	require.Nil(t, override)

	// Failure scenario (plan QA): a comp grant with a FUTURE ExpiresAt must NOT advance retention.
	// Reset to a clean active state, then project a future-dated comp grant.
	_, err = pool.Exec(ctx, `UPDATE users SET subscription_status='active', retention_started_at=NULL, retention_expires_at=NULL, plaid_cleanup_pending_at=NULL WHERE id=$1`, id)
	require.NoError(t, err)
	future := time.Now().Add(30 * 24 * time.Hour)
	state.Grants = []domain.RevenueCatGrant{{Key: "comp_premium", ExpiresAt: &future}}
	state.AccessUpdatedAt = time.Now()
	save()
	read()
	require.Equal(t, "active", status, "a future-dated comp grant must keep the user active")
	require.Nil(t, marker, "a future-dated comp grant must not arm the Plaid-cleanup marker")
	require.Nil(t, start, "a future-dated comp grant must not start a retention episode")
}