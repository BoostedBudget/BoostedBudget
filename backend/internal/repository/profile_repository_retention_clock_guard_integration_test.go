//go:build integration

package repository

// Integration tests for SetRetentionDates's write-time authorisation guard (Issue #1500 residual,
// Codex review, PR #1443 follow-up).
//
// The cancellation webhook re-checks authorisation immediately before this write
// (cancellationCleanupStillAuthorised, subscription_webhook.go), but a read-then-write gate can
// only NARROW the window it protects, never close it: an activation webhook committing between
// the gate's read and this UPDATE used to leave six-month cancellation retention/deletion dates
// stamped on a now-active, paying customer. The write is now its OWN guard — the authorising
// condition the caller knows (`subscription_status = <expected>`) travels into the statement's
// locked candidate CTE, exactly the shape the deleted CancelIfPastDueOccurrenceWithRetention (#1652) documents as
// "this method's WHERE clause is its OWN correctness mechanism".
//
// Both mechanisms are load-bearing and neither replaces the other: the Go gate short-circuits the
// per-item Plaid disconnect loop (work the SQL guard cannot stop), while the WHERE clause stops
// the write itself.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionClockGuard -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// newRetentionGuardUser creates a committed user row in the given subscription status and
// registers its cleanup. Cleanup deliberately uses context.Background(), NEVER t.Context(): the
// latter is cancelled before t.Cleanup runs, so the DELETE would fail silently and leak the row
// into a different test on this shared database.
func newRetentionGuardUser(t *testing.T, pool *pgxpool.Pool, repo ProfileRepository, tag, status string) *domain.User {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_retguard_" + tag + "_" + suffix,
		Email:              "retguard-" + tag + "-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	return user
}

// readRetentionClock returns the row's stored retention clock columns.
func readRetentionClock(t *testing.T, pool *pgxpool.Pool, userID string) (started, expires *time.Time) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT retention_started_at, retention_expires_at FROM users WHERE id = $1`, userID,
	).Scan(&started, &expires))
	return started, expires
}

// TestRetentionClockGuard_Integration_StatusChangedUnderTheGate_WritesNothing is THE headline
// test: the authorising state changed after the caller's Go-side gate passed (simulated by
// changing the row before the write lands, which is precisely what the gate cannot see), so the
// write must apply to nothing at all.
//
// Falsified by dropping `AND subscription_status = <expected>` from SetRetentionDates's candidate
// CTE: this test then goes RED (retention dates stamped on an active customer) while its sibling
// below stays green.
func TestRetentionClockGuard_Integration_StatusChangedUnderTheGate_WritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// The user WAS cancelled when the cancellation job's gate read them; an activation webhook has
	// since committed 'active'.
	user := newRetentionGuardUser(t, pool, repo, "changed", "cancelled")
	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)

	err = repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths)
	require.ErrorIs(t, err, ErrRetentionClockPreconditionLost,
		"a retention-clock write whose authorising condition no longer holds must fail distinguishably, never succeed silently")

	started, expires := readRetentionClock(t, pool, user.ID)
	assert.Nil(t, started, "no retention_started_at may be stamped on a user who is no longer cancelled")
	assert.Nil(t, expires, "no retention_expires_at may be stamped on a user who is no longer cancelled")
}

// TestRetentionClockGuard_Integration_StillCancelled_WritesClock is the mandatory sibling: the
// ordinary cancellation path still arms the clock exactly as before. Without it, the headline test
// above would pass just as well if the write had broken outright.
func TestRetentionClockGuard_Integration_StillCancelled_WritesClock(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := newRetentionGuardUser(t, pool, repo, "cancelled", "cancelled")

	before := dbNow(t, pool)
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths))
	after := dbNow(t, pool)

	started, expires := readRetentionClock(t, pool, user.ID)
	require.NotNil(t, started, "retention_started_at must be set on the ordinary cancellation path")
	require.NotNil(t, expires, "retention_expires_at must be set on the ordinary cancellation path")
	assertBetweenFence(t, before, after, *started, "retention_started_at must be ~now")
	assertBetweenFence(t, dbAddMonths(t, pool, before, testRetentionMonths), dbAddMonths(t, pool, after, testRetentionMonths), *expires,
		"retention_expires_at must be ~now+retentionMonths, computed via Postgres's make_interval")
}

// TestRetentionClockGuard_Integration_ActivationCommitsDuringLockWait_WritesNothing is the
// headline race in its REAL form, not simulated: a concurrent transaction holds the row's lock,
// flips the user to 'active' and commits WHILE this write is already in flight and blocked on that
// lock. Nothing the caller could have read beforehand — however late — could have seen this.
//
// It also pins the mechanism the guard depends on and which no simpler shape provides: the
// candidate CTE's `FOR NO KEY UPDATE` makes the status comparison happen against the row version
// that exists once the wait RESOLVES (Postgres follows the row's update chain for a locked SELECT),
// not the pre-wait version this statement first scanned. This is the same property
// the deleted CancelIfPastDueOccurrenceWithRetention's ROUND 23 restructure rests on — see
// profile_repository_finding2_lock_contention_integration_test.go's header for the empirical
// write-up.
func TestRetentionClockGuard_Integration_ActivationCommitsDuringLockWait_WritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := newRetentionGuardUser(t, pool, repo, "lockrace", "cancelled")

	// tx1 takes the row lock by performing the very write that revokes this flow's authorisation,
	// and holds it while the retention-clock write blocks behind it.
	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after Commit below

	_, err = tx1.Exec(ctx, `UPDATE users SET subscription_status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)

	var tx1PID int32
	require.NoError(t, tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&tx1PID))

	done := make(chan error, 1)
	go func() {
		done <- repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths)
	}()

	// waitUntilBlockedBy proves the write is genuinely waiting on THIS transaction's lock — not
	// merely that some backend is blocked somewhere — so the commit below really does land in the
	// middle of the write, which is the whole scenario.
	waitUntilBlockedBy(t, pool, tx1PID, 5*time.Second)
	require.NoError(t, tx1.Commit(ctx))

	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the retention-clock write to complete after the lock was released")
	}
	require.ErrorIs(t, err, ErrRetentionClockPreconditionLost,
		"the write must observe the activation that committed during its own lock wait, and refuse")

	started, expires := readRetentionClock(t, pool, user.ID)
	assert.Nil(t, started, "no retention_started_at may survive an activation that committed during the write's lock wait")
	assert.Nil(t, expires, "no retention_expires_at may survive an activation that committed during the write's lock wait")
}

// TestRetentionClockGuard_Integration_TrialPath_StillInactive_WritesClock covers the OTHER
// production caller. The legacy trial-expiry flow's (deleted in #1652) authorising condition is not the cancellation
// webhook's: it is the CAS immediately above its call (ExpireTrialIfNoOverride), which moves the
// user 'trial' -> 'inactive', so what must still hold at write time is 'inactive'.
func TestRetentionClockGuard_Integration_TrialPath_StillInactive_WritesClock(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := newRetentionGuardUser(t, pool, repo, "trialok", "trial")
	flipped, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, flipped, "the trial-expiry CAS must flip a plain trial user")

	before := dbNow(t, pool)
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusInactive), testRetentionMonths))
	after := dbNow(t, pool)

	started, expires := readRetentionClock(t, pool, user.ID)
	require.NotNil(t, started, "the trial-expiry path must still arm retention_started_at")
	require.NotNil(t, expires, "the trial-expiry path must still arm retention_expires_at")
	assertBetweenFence(t, before, after, *started, "retention_started_at must be ~now on the trial path")
	assertBetweenFence(t, dbAddMonths(t, pool, before, testRetentionMonths), dbAddMonths(t, pool, after, testRetentionMonths), *expires,
		"retention_expires_at must be ~now+retentionMonths on the trial path")
}

// TestRetentionClockGuard_Integration_TrialPath_ResubscribedUnderTheGate_WritesNothing is the
// trial path's own instance of the same race: the user resubscribes between the CAS that flipped
// them to 'inactive' and this write (the checkout webhook writes 'active' and clears the clock).
// Re-arming a six-month deletion clock on that paying customer is the same defect the cancellation
// path had, which is why this caller needs a guard too — just a different one.
func TestRetentionClockGuard_Integration_TrialPath_ResubscribedUnderTheGate_WritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := newRetentionGuardUser(t, pool, repo, "trialrace", "trial")
	flipped, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, flipped)

	_, err = pool.Exec(ctx, `UPDATE users SET subscription_status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)

	err = repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusInactive), testRetentionMonths)
	require.ErrorIs(t, err, ErrRetentionClockPreconditionLost)

	started, expires := readRetentionClock(t, pool, user.ID)
	assert.Nil(t, started, "no retention clock may be re-armed on a user who resubscribed")
	assert.Nil(t, expires, "no retention clock may be re-armed on a user who resubscribed")
}

// TestRetentionClockGuard_Integration_DistinguishesNotFoundFromPreconditionLost is the requirement
// that the two zero-rows-affected outcomes never collapse into one: the caller must be able to tell
// "someone else changed this user" (a legitimate stop) from "no such user" (a genuine failure),
// because only one of them is an error.
func TestRetentionClockGuard_Integration_DistinguishesNotFoundFromPreconditionLost(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// (a) No such user at all.
	missing := "00000000-0000-0000-0000-0000000f1500"
	err := repo.SetRetentionDates(ctx, missing, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths)
	require.ErrorIs(t, err, ErrUserNotFound, "an absent user must report ErrUserNotFound")
	assert.NotErrorIs(t, err, ErrRetentionClockPreconditionLost, "an absent user must NOT masquerade as a lost precondition")

	// (b) A soft-deleted user is equally "no such live row" — the pre-existing is_deleted scoping,
	// unchanged by the guard.
	deleted := newRetentionGuardUser(t, pool, repo, "softdel", "cancelled")
	_, execErr := pool.Exec(ctx, `UPDATE users SET is_deleted = TRUE WHERE id = $1`, deleted.ID)
	require.NoError(t, execErr)
	err = repo.SetRetentionDates(ctx, deleted.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths)
	require.ErrorIs(t, err, ErrUserNotFound, "a soft-deleted user must report ErrUserNotFound")

	// (c) A live user whose authorising state changed — the other outcome, reported differently.
	changed := newRetentionGuardUser(t, pool, repo, "changed2", "active")
	err = repo.SetRetentionDates(ctx, changed.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), testRetentionMonths)
	require.ErrorIs(t, err, ErrRetentionClockPreconditionLost, "a live user whose state moved on must report ErrRetentionClockPreconditionLost")
	assert.NotErrorIs(t, err, ErrUserNotFound, "a live user must NOT masquerade as missing")
}

// TestRetentionClockGuard_Integration_ZeroValueGuardFailsClosed pins the signature's own defence:
// the guard is required, and an unspecified one is REJECTED rather than silently defaulting to an
// unguarded write. A default that silently skips the check is exactly how the original defect
// existed, so the zero value must never be a working call.
func TestRetentionClockGuard_Integration_ZeroValueGuardFailsClosed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := newRetentionGuardUser(t, pool, repo, "zeroguard", "cancelled")

	err := repo.SetRetentionDates(ctx, user.ID, RetentionClockGuard(""), testRetentionMonths)
	require.ErrorIs(t, err, ErrRetentionClockGuardUnspecified)

	started, expires := readRetentionClock(t, pool, user.ID)
	assert.Nil(t, started, "an unspecified guard must write nothing")
	assert.Nil(t, expires, "an unspecified guard must write nothing")

	// The explicit opt-out, by contrast, works — that is the point of naming it.
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockUnguarded, testRetentionMonths))
	started, expires = readRetentionClock(t, pool, user.ID)
	assert.NotNil(t, started, "RetentionClockUnguarded must still arm the clock")
	assert.NotNil(t, expires, "RetentionClockUnguarded must still arm the clock")
}
