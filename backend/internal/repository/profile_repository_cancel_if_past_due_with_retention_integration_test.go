//go:build integration

package repository

// Integration tests for CancelIfPastDueWithRetention (Issue #1413 round-3 finding H5 residual).
//
// The earlier CancelIfPastDue + SetRetentionDates pair left a window where the CAS could
// succeed but the retention-clock write could still be lost — and since GetUsersInPastDue only
// selects subscription_status = 'past_due', the daily dunning job could never re-reach a user
// this already flipped to 'cancelled'. CancelIfPastDueWithRetention combines both writes into
// ONE statement so they land together or not at all. pgxmock cannot validate the WHERE predicate,
// RowsAffected semantics, or that BOTH columns actually changed together — a real DB is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestCancelIfPastDueWithRetention_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestCancelIfPastDueWithRetention_Integration_AtomicFlipAndClock proves the core H5 fix: a
// single call flips subscription_status AND sets both retention timestamps together.
func TestCancelIfPastDueWithRetention_Integration_AtomicFlipAndClock(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_atomic_" + suffix,
		Email:              "atomic-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	started := time.Now().Truncate(time.Microsecond)
	expires := started.AddDate(0, 6, 0)

	flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, started, expires)
	require.NoError(t, err)
	assert.True(t, flipped, "an eligible past_due user must flip")

	var (
		status     string
		retStarted *time.Time
		retExpires *time.Time
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_started_at, retention_expires_at FROM users WHERE id = $1`,
		user.ID,
	).Scan(&status, &retStarted, &retExpires))

	assert.Equal(t, "cancelled", status, "subscription_status must flip to cancelled")
	require.NotNil(t, retStarted, "retention_started_at must be set in the SAME call")
	require.NotNil(t, retExpires, "retention_expires_at must be set in the SAME call")
	assert.WithinDuration(t, started, *retStarted, time.Microsecond)
	assert.WithinDuration(t, expires, *retExpires, time.Microsecond)
}

// TestCancelIfPastDueWithRetention_Integration_SkipsNonPastDue proves the predicate still guards
// correctness: a non-past_due user is untouched, including its retention columns. Covers all
// three non-past_due statuses (active, cancelled, inactive) — only past_due rows match the WHERE
// predicate.
func TestCancelIfPastDueWithRetention_Integration_SkipsNonPastDue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	for _, status := range []string{"active", "cancelled", "inactive"} {
		s := status
		t.Run(s, func(t *testing.T) {
			user := &domain.User{
				ClerkUserID:        "clerk_notpd_" + s + "_" + suffix,
				Email:              "notpd-" + s + "-" + suffix + "@example.com",
				SubscriptionTier:   "premium",
				SubscriptionStatus: s,
				PrimaryRegion:      "iad",
				IsActive:           true,
			}
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, repo.CreateUserTx(ctx, tx, user))
			require.NoError(t, tx.Commit(ctx))
			t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

			now := time.Now()
			flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, now, now.AddDate(0, 6, 0))
			require.NoError(t, err)
			assert.False(t, flipped, "a %s (non-past_due) user must not flip", s)

			var got string
			var retExpires *time.Time
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT subscription_status, retention_expires_at FROM users WHERE id = $1`, user.ID,
			).Scan(&got, &retExpires))
			assert.Equal(t, s, got, "status must be unchanged for a %s user", s)
			assert.Nil(t, retExpires, "retention_expires_at must stay NULL when the CAS misses")
		})
	}
}

// TestCancelIfPastDueWithRetention_Integration_ExpiredOverride_StillFlips proves the negative
// direction of noActiveManualOverridePredicate: an override that has already LAPSED must not
// block the flip (or the retention-clock write) — an over-tight predicate that treats any
// override (live or expired) as a block would silently stop cancellation, and the retention
// clock, forever for a user who was comped in the past. Mirrors
// HasActiveManualOverride()'s boundary semantics.
func TestCancelIfPastDueWithRetention_Integration_ExpiredOverride_StillFlips(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_cas_expired_" + suffix,
		Email:              "cas-expired-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	started := time.Now().Truncate(time.Microsecond)
	expires := started.AddDate(0, 6, 0)
	flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, started, expires)
	require.NoError(t, err)
	assert.True(t, flipped, "CancelIfPastDueWithRetention must still flip a user whose override has already expired")

	var status string
	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires))
	assert.Equal(t, "cancelled", status)
	require.NotNil(t, retExpires, "retention_expires_at must be set even though the user was previously comped")
	assert.WithinDuration(t, expires, *retExpires, time.Microsecond)
}

// TestCancelIfPastDueWithRetention_Integration_SkipsActiveOverride proves the
// noActiveManualOverridePredicate is still enforced: a comped past_due user is not flipped, and
// no retention clock is started for them either.
func TestCancelIfPastDueWithRetention_Integration_SkipsActiveOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_comped_" + suffix,
		Email:              "comped-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	future := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	now := time.Now()
	flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, now, now.AddDate(0, 6, 0))
	require.NoError(t, err)
	assert.False(t, flipped, "an actively comped past_due user must not flip")

	var status string
	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires))
	assert.Equal(t, "past_due", status, "a comped user must stay past_due, not flip to cancelled")
	assert.Nil(t, retExpires, "no retention clock must start for a comped user")
}

// TestCancelIfPastDueWithRetention_Integration_SecondCallIsNoop proves repeat-call safety: after
// a successful flip, a second call (simulating a retry) must not error and must not perturb the
// already-set retention clock.
func TestCancelIfPastDueWithRetention_Integration_SecondCallIsNoop(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_repeat_" + suffix,
		Email:              "repeat-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	firstStarted := time.Now().Truncate(time.Microsecond)
	firstExpires := firstStarted.AddDate(0, 6, 0)
	flipped, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, firstStarted, firstExpires)
	require.NoError(t, err)
	require.True(t, flipped)

	secondStarted := time.Now().Add(time.Hour)
	secondExpires := secondStarted.AddDate(0, 6, 0)
	flipped2, err := repo.CancelIfPastDueWithRetention(ctx, user.ID, secondStarted, secondExpires)
	require.NoError(t, err)
	assert.False(t, flipped2, "a second call against an already-cancelled user must miss the CAS")

	var retStarted *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_started_at FROM users WHERE id = $1`, user.ID).Scan(&retStarted))
	require.NotNil(t, retStarted)
	assert.WithinDuration(t, firstStarted, *retStarted, time.Microsecond, "the second call must not overwrite the first call's retention clock")
}
