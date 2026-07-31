//go:build integration

package repository

// Integration tests for Issue #1413 Task 9: ProfileRepository.GetUsersWithExpiredManualTierOverride,
// the query backing the daily job that expires lapsed manual tier overrides (admin comps) —
// clearing them, re-arming retention, and re-syncing Clerk via the same
// AdminUserRepository.RemoveUserTierOverride transition admin-initiated removal uses (Issue
// #1414 / Task 7). pgxmock cannot validate this predicate (it only matches query text), so a
// wrong boundary operator or a missing is_deleted guard would pass silently without a real
// database.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetUsersWithExpiredManualTierOverride -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// createManualTierExpiryFixtureUser inserts a user row with the given manual-override columns
// (and, optionally, is_deleted) and registers cleanup.
func createManualTierExpiryFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
	isDeleted bool,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_mte_" + suffix,
		Email:              "mte-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET manual_tier_override = $1,
		    manual_tier_ends_at = $2,
		    is_deleted = $3
		WHERE id = $4`,
		manualTierOverride, manualTierEndsAt, isDeleted, user.ID,
	)
	require.NoError(t, err)

	user.ManualTierOverride = manualTierOverride
	user.ManualTierEndsAt = manualTierEndsAt
	user.IsDeleted = isDeleted
	return user
}

// TestGetUsersWithExpiredManualTierOverride_Integration_SelectsExpiredExcludesLiveAndDeleted
// proves the required outcomes in one shot: an override whose manual_tier_ends_at has passed IS
// selected; a still-live override (future ends_at) is EXCLUDED; a soft-deleted user with an
// otherwise-expired override is EXCLUDED; a user with no override at all is EXCLUDED (the query
// must not be a no-op); and a PERMANENT override (non-nil override, nil ends_at — never expires)
// is EXCLUDED.
func TestGetUsersWithExpiredManualTierOverride_Integration_SelectsExpiredExcludesLiveAndDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	household := "household_2"
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	expired := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_expired", &household, &past, false)
	live := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_live", &household, &future, false)
	deletedExpired := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_deleted", &household, &past, true)
	noOverride := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_none", nil, nil, false)
	permanent := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_permanent", &household, nil, false)
	// nilOverridePastEndsAt: no override present at all, but manual_tier_ends_at happens to be
	// set in the past (columns are independent — no DB CHECK constraint ties them together).
	// There is nothing to expire here; a predicate that forgets the override-is-set guard and
	// only checks manual_tier_ends_at would wrongly select this row.
	nilOverridePastEndsAt := createManualTierExpiryFixtureUser(t, ctx, pool, repo, suffix+"_nil_override_past", nil, &past, false)

	users, err := repo.GetUsersWithExpiredManualTierOverride(ctx)
	require.NoError(t, err)

	ids := make(map[string]bool, len(users))
	for _, u := range users {
		ids[u.ID] = true
	}

	if !ids[expired.ID] {
		t.Errorf("user %s with an EXPIRED override must be selected, but was excluded", expired.ID)
	}
	if ids[live.ID] {
		t.Errorf("user %s with a LIVE (future ends_at) override must be EXCLUDED, but was selected", live.ID)
	}
	if ids[deletedExpired.ID] {
		t.Errorf("soft-deleted user %s must be EXCLUDED even with an expired override, but was selected", deletedExpired.ID)
	}
	if ids[noOverride.ID] {
		t.Errorf("user %s with no override at all must be EXCLUDED, but was selected", noOverride.ID)
	}
	if ids[permanent.ID] {
		t.Errorf("user %s with a PERMANENT override (nil ends_at) must be EXCLUDED — it never expires on its own, but was selected", permanent.ID)
	}
	if ids[nilOverridePastEndsAt.ID] {
		t.Errorf("user %s with NO override set (despite a past manual_tier_ends_at) must be EXCLUDED — nothing to expire, but was selected", nilOverridePastEndsAt.ID)
	}
}

// TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary proves the SQL
// predicate treats manual_tier_ends_at == NOW() as still ACTIVE (excluded), matching
// domain.User.HasActiveManualOverride()'s `!ends_at.Before(now)` semantics (true AT the expiry
// instant, not only strictly before it). Runs the exact production predicate
// (expiredManualTierOverridePredicate) in the SAME transaction as the NOW() read so both see the
// identical, transaction-stable timestamp — a guaranteed tie, immune to real-world delay between
// the fixture write and the assertion query.
func TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("boundary_%d", time.Now().UnixNano())
	household := "household_2"
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	boundaryTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer boundaryTx.Rollback(ctx) //nolint:errcheck

	var txNow time.Time
	require.NoError(t, boundaryTx.QueryRow(ctx, "SELECT NOW()").Scan(&txNow))

	_, err = boundaryTx.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`, household, txNow, user.ID)
	require.NoError(t, err)

	var stillSelected bool
	err = boundaryTx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND `+expiredManualTierOverridePredicate+`)`, user.ID).Scan(&stillSelected)
	require.NoError(t, err)

	require.False(t, stillSelected, "manual_tier_ends_at == NOW() must be treated as still ACTIVE (excluded); a '<=' operator would wrongly select this row")
}
