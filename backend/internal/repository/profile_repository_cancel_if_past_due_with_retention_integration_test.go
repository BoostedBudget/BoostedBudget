//go:build integration

package repository

// Integration tests for CancelIfPastDueOccurrenceWithRetention (Issue #1413 round-3 finding H5
// residual; occurrence-scoped per round-4 finding H1).
//
// The earlier CancelIfPastDue + SetRetentionDates pair left a window where the CAS could
// succeed but the retention-clock write could still be lost — and since GetUsersInPastDue only
// selects subscription_status = 'past_due', the daily dunning job could never re-reach a user
// this already flipped to 'cancelled'. CancelIfPastDueOccurrenceWithRetention combines both writes
// into ONE statement so they land together or not at all. pgxmock cannot validate the WHERE
// predicate, RowsAffected semantics, or that BOTH columns actually changed together — a real DB is
// required.
//
// Issue #1442: the non-occurrence predecessor, CancelIfPastDueWithRetention, was deleted for
// having zero production callers (past_due_service.go's PastDueProfileRepository consumer
// interface never declared it) — and for a second, independent reason: it wrote
// retention_expires_at from a caller-supplied Go time.Time built with AddDate, which disagrees
// with Postgres's make_interval on month-end dates in the over-retention direction (the wrong
// direction for a GDPR deletion deadline). Every test below targets
// CancelIfPastDueOccurrenceWithRetention, the sole remaining, occurrence-scoped, Postgres-clamped
// writer of this transition.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestCancelIfPastDueOccurrenceWithRetention_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestCancelIfPastDueOccurrenceWithRetention_Integration_MatchingOccurrence_Flips proves the
// happy path: CASing with the occurrence the caller actually claimed flips the user, starts the
// retention clock, and starts plaid_cleanup_pending_at (Issue #1413 round-10 — the Plaid-disconnect
// grace-window anchor), all in the SAME statement.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_MatchingOccurrence_Flips(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_occmatch_" + suffix,
		Email:              "occmatch-" + suffix + "@example.com",
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

	pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueStarted))

	// Task NA2 (Issue #1413 follow-up on PR #1443): retentionMonths replaces a Go-computed
	// retentionExpires time.Time — retention_expires_at is now computed entirely in Postgres via
	// clock_timestamp() + make_interval(months => retentionMonths), so the expectation below is
	// fenced with dbNow/dbAddMonths (not time.Now()/AddDate), exactly like
	// profile_repository_set_retention_dates_integration_test.go's
	// TestSetRetentionDates_Integration_ArmsClockWithPostgresArithmetic — calendar-immune by
	// construction rather than merely tolerant of drift. Uses testRetentionMonths (9), the same
	// constant that file uses and deliberately not the config default of 6, for the same reason:
	// a wider variety of month-end landings across the year to exercise.
	started := time.Now().Truncate(time.Microsecond)
	const retentionMonths = testRetentionMonths
	before := dbNow(t, pool)
	flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, started, retentionMonths)
	require.NoError(t, err)
	after := dbNow(t, pool)
	assert.True(t, flipped, "CASing with the occurrence the caller actually claimed must flip")

	var (
		status      string
		retExpires  *time.Time
		plaidMarker *time.Time
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at, plaid_cleanup_pending_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires, &plaidMarker))
	assert.Equal(t, "cancelled", status)
	require.NotNil(t, retExpires, "retention_expires_at must be set in the SAME call")
	assertBetweenFence(t, dbAddMonths(t, pool, before, retentionMonths), dbAddMonths(t, pool, after, retentionMonths), *retExpires,
		"retention_expires_at must be ~now+retentionMonths, computed via Postgres's make_interval")
	require.NotNil(t, plaidMarker, "plaid_cleanup_pending_at must be set in the SAME call")
	assert.False(t, plaidMarker.Before(before.Add(-time.Second)), "plaid_cleanup_pending_at must be set to approximately now")
}

// TestCancelIfPastDueOccurrenceWithRetention_Integration_StaleOccurrence_DoesNotFlip is THE H1
// regression proof at the repository layer: a worker holding an EARLIER cycle's timestamp
// (staleOccurrence) must not flip a user whose row has already moved on to a NEWER cycle
// (currentOccurrence) — even though status and override still pass. Before this fix (a predicate
// checking only status+override), this CAS would have incorrectly flipped the user mid-cycle-B,
// before cycle B's own grace period expired.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_StaleOccurrence_DoesNotFlip(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_occstale_" + suffix,
		Email:              "occstale-" + suffix + "@example.com",
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

	staleOccurrence := time.Now().Add(-60 * 24 * time.Hour).Truncate(time.Microsecond) // cycle A — what the stale worker claimed
	currentOccurrence := time.Now().Add(-1 * time.Hour).Truncate(time.Microsecond)     // cycle B — the row's CURRENT cycle
	_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1, subscription_status = 'past_due' WHERE id = $2`, currentOccurrence, user.ID)
	require.NoError(t, err)

	started := time.Now().Truncate(time.Microsecond)
	flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, staleOccurrence, started, 6)
	require.NoError(t, err)
	assert.False(t, flipped, "a stale occurrence must not flip a user whose row has moved on to a newer cycle")

	var status string
	var retExpires *time.Time
	var pastDueStartedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at, past_due_started_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires, &pastDueStartedAt))
	assert.Equal(t, "past_due", status, "the user must remain past_due — cycle B's own grace period has not been evaluated")
	assert.Nil(t, retExpires, "no retention clock must start off a stale occurrence")
	require.NotNil(t, pastDueStartedAt)
	assert.WithinDuration(t, currentOccurrence, *pastDueStartedAt, time.Microsecond, "cycle B's timestamp must be untouched")
}

// TestCancelIfPastDueOccurrenceWithRetention_Integration_SkipsActiveOverride closes a gap left by
// the two tests above (Issue #1413/#1443 gap analysis, PR #1443 AC9 pattern-2 finding):
// CancelIfPastDueOccurrenceWithRetention splices its OWN copy of noActiveManualOverridePredicate —
// nothing before this test proved THAT copy actually blocks a comped user. Both tests above only
// vary the occurrence (past_due_started_at) axis, on an always-uncomped fixture. Uses the MATCHING
// occurrence (so the occurrence discriminator itself passes) plus a live override, isolating the
// override predicate as the only thing that can prevent the flip.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_SkipsActiveOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_occcomp_" + suffix,
		Email:              "occcomp-" + suffix + "@example.com",
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

	pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueStarted))

	future := time.Now().Add(24 * time.Hour) // still active
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	started := time.Now().Truncate(time.Microsecond)
	flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, started, 6)
	require.NoError(t, err)
	assert.False(t, flipped, "an actively comped past_due user must not flip, even with the exact matching occurrence")

	var status string
	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires))
	assert.Equal(t, "past_due", status, "a comped user must stay past_due, not flip to cancelled")
	assert.Nil(t, retExpires, "no retention clock must start for a comped user")
}

// --- Issue #1442 ports: scenarios the deleted non-occurrence predecessor used to pin, with no
// occurrence-side equivalent before this file.

// TestCancelIfPastDueOccurrenceWithRetention_Integration_SkipsNonPastDue ports the deleted
// non-occurrence predecessor's SkipsNonPastDue status-matrix proof (Issue #1442): covers all three
// non-past_due statuses (active, cancelled, inactive) for the occurrence-scoped CAS, with a
// matching past_due_started_at value so the status guard — not the occurrence guard — is what's
// under test.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_SkipsNonPastDue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	for _, status := range []string{"active", "cancelled", "inactive"} {
		s := status
		t.Run(s, func(t *testing.T) {
			user := &domain.User{
				ClerkUserID:        "clerk_occnotpd_" + s + "_" + suffix,
				Email:              "occnotpd-" + s + "-" + suffix + "@example.com",
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

			pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
			_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1 WHERE id = $2`, pastDueStarted, user.ID)
			require.NoError(t, err)

			started := time.Now().Truncate(time.Microsecond)
			flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, started, 6)
			require.NoError(t, err)
			assert.False(t, flipped, "a %s (non-past_due) user must not flip, even with a matching occurrence", s)

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

// TestCancelIfPastDueOccurrenceWithRetention_Integration_ExpiredOverride_StillFlips ports the
// deleted non-occurrence predecessor's ExpiredOverride_StillFlips proof (Issue #1442): the negative
// direction of noActiveManualOverridePredicate — an override that has already LAPSED must not
// block the occurrence-scoped flip (or the retention-clock write) either. An over-tight predicate
// that treats any override (live or expired) as a block would silently stop cancellation, and the
// retention clock, forever for a user who was comped in the past.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_ExpiredOverride_StillFlips(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_occ_cas_expired_" + suffix,
		Email:              "occ-cas-expired-" + suffix + "@example.com",
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

	pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueStarted))

	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	started := time.Now().Truncate(time.Microsecond)
	flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, started, 6)
	require.NoError(t, err)
	assert.True(t, flipped, "CancelIfPastDueOccurrenceWithRetention must still flip a user whose override has already expired")

	var status string
	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT subscription_status, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&status, &retExpires))
	assert.Equal(t, "cancelled", status)
	require.NotNil(t, retExpires, "retention_expires_at must be set even though the user was previously comped")
}

// TestCancelIfPastDueOccurrenceWithRetention_Integration_SecondCallIsNoop ports the deleted
// non-occurrence predecessor's SecondCallIsNoop proof (Issue #1442): repeat-call safety. After a
// successful flip, a second call (simulating a retry, even one that still names the SAME
// originally-claimed occurrence) must not error, must miss the CAS — the row's subscription_status
// has already moved to 'cancelled' — and must not perturb the already-set retention clock. This
// retry-safety property matters more for the occurrence-scoped variant than it did for its deleted
// predecessor, since this is the one past_due_service.go actually calls in production.
func TestCancelIfPastDueOccurrenceWithRetention_Integration_SecondCallIsNoop(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_occ_repeat_" + suffix,
		Email:              "occ-repeat-" + suffix + "@example.com",
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

	pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueStarted))

	firstStarted := time.Now().Truncate(time.Microsecond)
	flipped, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, firstStarted, 6)
	require.NoError(t, err)
	require.True(t, flipped)

	var firstRetExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&firstRetExpires))
	require.NotNil(t, firstRetExpires)

	secondStarted := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	flipped2, err := repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, secondStarted, 9)
	require.NoError(t, err)
	assert.False(t, flipped2, "a second call against an already-cancelled user must miss the CAS, even naming the same original occurrence")

	var retStarted *time.Time
	var retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT retention_started_at, retention_expires_at FROM users WHERE id = $1`, user.ID).Scan(&retStarted, &retExpires))
	require.NotNil(t, retStarted)
	assert.WithinDuration(t, firstStarted, *retStarted, time.Microsecond, "the second call must not overwrite the first call's retention clock")
	assert.Equal(t, *firstRetExpires, *retExpires, "the second call must not overwrite the first call's retention_expires_at")
}
