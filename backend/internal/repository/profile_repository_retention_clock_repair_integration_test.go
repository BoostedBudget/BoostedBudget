//go:build integration

package repository

// Integration tests for GetUsersNeedingRetentionClockRepair / RepairRetentionClock (Issue #1413
// round-3 finding H5 residual, belt-and-braces repair path).
//
// The deleted CancelIfPastDueOccurrenceWithRetention (#1652) closed the specific race on the past-due grace-cancel path going
// forward, but this repair path also catches: (a) any row already stuck read-only-status with a
// NULL retention_expires_at from before that fix existed, and (b) any other, currently-unknown
// path that might leave a user in this state. pgxmock cannot validate the WHERE predicates or
// RowsAffected semantics — a real DB is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionClockRepair_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestRetentionClockRepair_Integration_FindsAndRepairsCancelledUserWithNullClock(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_repair_" + suffix,
		Email:              "repair-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	// Simulate the H5 residual: cancelled, but retention_expires_at is NULL (as
	// CreateUserTx leaves it by default — no separate mutation needed).
	var initialExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&initialExpires))
	require.Nil(t, initialExpires, "fixture must start with a NULL retention_expires_at")

	candidates, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "the cancelled user with a NULL retention clock must be a repair candidate")

	// Task NA2 (Issue #1413 follow-up on PR #1443): retentionMonths replaces a Go-computed
	// expiresAt time.Time — retention_expires_at is now computed entirely in Postgres via
	// clock_timestamp() + make_interval(months => retentionMonths), so the expectation below is
	// fenced with dbNow/dbAddMonths (not time.Now()/AddDate), exactly like
	// profile_repository_set_retention_dates_integration_test.go's
	// TestSetRetentionDates_Integration_ArmsClockWithPostgresArithmetic — calendar-immune by
	// construction rather than merely tolerant of drift. retention_started_at, by contrast, stays
	// a plain Go-computed value ($2, unchanged) — see RepairRetentionClock's own doc comment. Uses
	// testRetentionMonths (9), deliberately not the config default of 6, for the same reason that
	// constant's own file uses it: a wider variety of month-end landings across the year.
	started := time.Now().Truncate(time.Microsecond)
	const retentionMonths = testRetentionMonths
	before := dbNow(t, pool)
	repaired, err := repo.RepairRetentionClock(ctx, user.ID, started, retentionMonths)
	require.NoError(t, err)
	after := dbNow(t, pool)
	assert.True(t, repaired, "the repair write must succeed for a genuine candidate")

	var (
		retStarted *time.Time
		retExpires *time.Time
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retention_started_at, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&retStarted, &retExpires))
	require.NotNil(t, retStarted)
	require.NotNil(t, retExpires)
	assert.WithinDuration(t, started, *retStarted, time.Microsecond)
	assertBetweenFence(t, dbAddMonths(t, pool, before, retentionMonths), dbAddMonths(t, pool, after, retentionMonths), *retExpires,
		"retention_expires_at must be ~now+retentionMonths, computed via Postgres's make_interval")

	// After repair, the user must no longer be a candidate.
	candidatesAfter, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidatesAfter {
		assert.NotEqual(t, user.ID, c.ID, "a repaired user must no longer be a repair candidate")
	}
}

func TestRetentionClockRepair_Integration_ExcludesUserWithExistingClock(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_hasclock_" + suffix,
		Email:              "hasclock-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	// RetentionClockUnguarded: fixture setup for the repair pass, not a subscription transition.
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockUnguarded, 6))
	// Read the persisted value back rather than trusting a Go-computed expectation — SetRetentionDates
	// now computes retention_expires_at entirely in Postgres (clock_timestamp() + make_interval), so
	// asserting against a Go time.Now().AddDate(0, 6, 0) value would only coincidentally agree (and
	// would silently diverge on a month-end run, e.g. the 31st).
	var existingExpiresPtr *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&existingExpiresPtr))
	require.NotNil(t, existingExpiresPtr, "SetRetentionDates must have written a non-NULL retention_expires_at")
	existingExpires := *existingExpiresPtr

	candidates, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an existing (non-NULL) retention clock must never be a repair candidate")
	}

	// RepairRetentionClock itself must also refuse to clobber the existing clock even if called
	// directly (defense in depth against a stale candidate list).
	repaired, err := repo.RepairRetentionClock(ctx, user.ID, time.Now(), 1)
	require.NoError(t, err)
	assert.False(t, repaired, "RepairRetentionClock must refuse to overwrite an existing, non-NULL clock")

	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&retExpires))
	require.NotNil(t, retExpires)
	assert.WithinDuration(t, existingExpires, *retExpires, time.Microsecond, "the original clock must be untouched")
}

func TestRetentionClockRepair_Integration_ExcludesActiveUser(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_active_" + suffix,
		Email:              "active-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	candidates, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "an active user (not read-only status) must never be a repair candidate")
	}

	repaired, err := repo.RepairRetentionClock(ctx, user.ID, time.Now(), 6)
	require.NoError(t, err)
	assert.False(t, repaired, "RepairRetentionClock must refuse to write for a non-read-only-status user")
}

func TestRetentionClockRepair_Integration_ExcludesComped(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_repaircomp_" + suffix,
		Email:              "repaircomp-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	// Lapsed override (ends in the past) — noManualOverrideAtAllPredicate excludes ANY override,
	// live or lapsed, mirroring the other retention queries.
	past := time.Now().Add(-24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, past, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with ANY manual_tier_override (live or lapsed) must never be a repair candidate")
	}
}

// TestRetentionClockRepair_Integration_RepairItselfRefusesComped closes a gap left by the test
// above (Issue #1413/#1443 gap analysis, PR #1443 AC9 pattern-2 finding):
// TestRetentionClockRepair_Integration_ExcludesComped only proves
// GetUsersNeedingRetentionClockRepair's OWN copy of noManualOverrideAtAllPredicate excludes a
// comped candidate from the SELECT — it never calls RepairRetentionClock itself, so that
// method's OWN predicate splice (profile_repository.go:2089) has never been directly exercised
// with a comped fixture. TestRetentionClockRepair_Integration_ExcludesUserWithExistingClock and
// ..._ExcludesActiveUser both already call RepairRetentionClock directly as defense-in-depth
// (mirroring a stale candidate list); this test does the same for the override axis: a comped,
// NULL-clock, cancelled user must have the UPDATE itself refuse to write, not merely be absent
// from the candidate list.
func TestRetentionClockRepair_Integration_RepairItselfRefusesComped(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_repaircompdirect_" + suffix,
		Email:              "repaircompdirect-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, overrideEndsAt, user.ID)
	require.NoError(t, err)

	repaired, err := repo.RepairRetentionClock(ctx, user.ID, time.Now(), 6)
	require.NoError(t, err)
	assert.False(t, repaired, "RepairRetentionClock must refuse to start the retention clock for a user with a live manual tier override, even called directly against a NULL-clock candidate")

	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&retExpires))
	assert.Nil(t, retExpires, "no retention clock must be started for a comped user")
}

func TestRetentionClockRepair_Integration_ExcludesDeletionPending(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_delpending_" + suffix,
		Email:              "delpending-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))

	candidates, err := repo.GetUsersNeedingRetentionClockRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an outstanding GDPR deletion request must never be a repair candidate")
	}
}
