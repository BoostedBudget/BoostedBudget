//go:build integration

package repository

// Issue #1413 Finding 2 (High, Codex review, PR #1443 follow-up) lock-contention regression tests.
//
// Five writers of plaid_cleanup_pending_at — UpdateSubscriptionFromPaddle, UpdateTrialStatus,
// CancelIfPastDueWithRetention, CancelIfPastDueOccurrenceWithRetention, ExpireTrialIfNoOverride —
// were restructured from a bare `UPDATE ... SET plaid_cleanup_pending_at = COALESCE(..., NOW())`
// into a `WITH candidate AS (SELECT ... FOR NO KEY UPDATE) UPDATE ... FROM candidate` shape, mirroring
// StampMissingPlaidCleanupMarkers's own ROUND 21/23 fix (profile_repository.go) — see that method's
// doc comment for the full empirically-established mechanism this restructure depends on: a bare
// UPDATE's SET-list (including a volatile clock_timestamp() call) is computed ONCE, against the row
// version obtained BEFORE any row-lock wait, and is NOT recomputed once the wait resolves — so
// neither a bare NOW() nor a bare clock_timestamp() substitution closes the backdating bug; only
// moving the lock acquisition into its own, earlier CTE step does.
//
// Each test below proves ONE writer's restructured COALESCE(plaid_cleanup_pending_at,
// clock_timestamp()) reflects real, post-lock-wait wall-clock time — using the SAME real
// synchronization barrier (waitUntilBlockedBy, admin_user_repository_remove_tier_override_integration_test.go)
// as StampMissingPlaidCleanupMarkers's own equivalent test, per Issue #1413 Finding 4 (Low, Codex
// review): the barrier must confirm the SPECIFIC backend running the writer under test is blocked,
// not merely that some backend somewhere is in a lock wait.
//
// Falsifiability (Issue #1413's own standing requirement, sharpened by this round's brief): each of
// these five tests was verified RED against BOTH of the following, by temporarily reverting the
// corresponding writer in profile_repository.go and re-running — never against only one probe, since
// a probe that only catches the bare-NOW() case cannot distinguish a real fix from a cosmetic one:
//   (a) the ORIGINAL bare `UPDATE ... SET plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, NOW())`
//       (no CTE at all) — fails because NOW()/transaction_timestamp() is fixed at the (here, implicit)
//       transaction's BEGIN, before the statement's own row-lock wait even starts.
//   (b) the NAIVE `UPDATE ... SET plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())`
//       (still no CTE — a bare function-name swap) — fails for the SAME reason StampMissingPlaidCleanupMarkers's
//       own doc comment proves empirically: a bare UPDATE's SET-list, including a volatile call, is
//       evaluated against the pre-lock-wait row version and is never re-evaluated once the wait
//       resolves, so probe (b) backdates identically to probe (a) despite the different function name.
// Each probe was run three times per writer against each broken variant; every run failed the
// post-wait assertion below deterministically (never flaky) — see this round's report for the exact
// counts.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestFinding2LockContention -tags integration -v

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

// runWriterUnderLockContention holds a real FOR UPDATE lock on userID via a dedicated second
// connection (tx1), runs fn concurrently in a goroutine, blocks via waitUntilBlockedBy until fn's
// OWN backend is confirmed genuinely waiting on tx1's lock specifically (Issue #1413 Finding 4 — not
// merely "some backend is blocked somewhere"), holds the lock postBlockHoldDuration longer to widen
// the assertion margin (not itself load-bearing for correctness — waitUntilBlockedBy already proved
// the block is real), then releases it and waits for fn to return.
//
// Returns startFence (captured right after the lock is acquired, before fn is launched) and
// releaseFence (captured right before the lock is released) — both real Postgres clock reads
// (dbNow), never a Go-side time.Now(), so the caller can fence the writer's resulting timestamp the
// same way every other lock-contention test in this package already does.
func runWriterUnderLockContention(
	t *testing.T, pool *pgxpool.Pool, userID string, postBlockHoldDuration time.Duration, fn func() error,
) (startFence, releaseFence time.Time, fnErr error) {
	t.Helper()
	ctx := context.Background()

	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after Commit below

	var locked string
	require.NoError(t, tx1.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&locked))

	var tx1PID int32
	require.NoError(t, tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&tx1PID))

	startFence = dbNow(t, pool)

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	waitUntilBlockedBy(t, pool, tx1PID, 5*time.Second)
	time.Sleep(postBlockHoldDuration)
	releaseFence = dbNow(t, pool)
	require.NoError(t, tx1.Commit(ctx))

	select {
	case fnErr = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the writer call to complete after the lock was released")
	}
	return startFence, releaseFence, fnErr
}

// assertMarkerReflectsPostWaitTime is the shared assertion every test below makes: the marker must
// not be before releaseFence (proving clock_timestamp() was evaluated post-wait, not backdated to
// this statement's own pre-wait scan) and must be meaningfully after startFence (proving the wait
// actually happened at all, not merely that the assertion window is wide).
func assertMarkerReflectsPostWaitTime(t *testing.T, startFence, releaseFence time.Time, postBlockHoldDuration time.Duration, marker *time.Time) {
	t.Helper()
	require.NotNil(t, marker, "plaid_cleanup_pending_at must be set")
	assert.False(t, marker.Before(releaseFence),
		"plaid_cleanup_pending_at must reflect statement-execution (wall) time, at or after the lock's release (%v) — got %v, which would mean this writer used a pre-wait timestamp instead and was backdated by the entire lock wait", releaseFence, *marker)
	assert.True(t, marker.After(startFence.Add(postBlockHoldDuration/2)),
		"plaid_cleanup_pending_at must NOT be anywhere near this statement's own pre-wait start (%v) — got %v", startFence, *marker)
}

const finding2PostBlockHoldDuration = 2 * time.Second

// TestFinding2LockContention_UpdateSubscriptionFromPaddle_ClockTimestampReflectsWallTimeAcrossLockWait
// covers UpdateSubscriptionFromPaddle's ROUND 23 restructure — a live Paddle-webhook writer. Exercises
// the "no subscriptionID" query variant (subscriptionID == nil); the "with subscriptionID" variant
// shares the IDENTICAL CTE + FOR NO KEY UPDATE + clock_timestamp() shape, differing only in which
// extra columns are set, so this one call path validates the mechanism for both.
func TestFinding2LockContention_UpdateSubscriptionFromPaddle_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_paddle_" + suffix,
		Email:              "f2-paddle-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active", // NOT already repair-eligible: a genuine transition into ineligibility
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	startedAt := time.Now()
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		return repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", &startedAt, nil, nil)
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}

// TestFinding2LockContention_UpdateTrialStatus_ClockTimestampReflectsWallTimeAcrossLockWait covers
// UpdateTrialStatus's ROUND 23 restructure. This writer is latent in production (its only caller
// always passes "trial", which takes the ELSE/clearing branch, never this COALESCE branch — see the
// method's own doc comment) — this test calls it directly with an eligible status ("cancelled") to
// exercise the COALESCE branch regardless, proving the fix "for consistency" as the brief requires.
func TestFinding2LockContention_UpdateTrialStatus_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_trial_" + suffix,
		Email:              "f2-trial-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	trialStarted := time.Now()
	trialEnds := trialStarted.Add(30 * 24 * time.Hour)
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		return repo.UpdateTrialStatus(ctx, user.ID, "cancelled", &trialStarted, &trialEnds, true)
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}

// TestFinding2LockContention_CancelIfPastDueWithRetention_ClockTimestampReflectsWallTimeAcrossLockWait
// covers CancelIfPastDueWithRetention's ROUND 23 restructure — one of the three genuine CAS writers.
func TestFinding2LockContention_CancelIfPastDueWithRetention_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_pastdue_" + suffix,
		Email:              "f2-pastdue-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	started := time.Now()
	expires := started.AddDate(0, 6, 0)
	var flipped bool
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		var innerErr error
		flipped, innerErr = repo.CancelIfPastDueWithRetention(ctx, user.ID, started, expires)
		return innerErr
	})
	require.NoError(t, callErr)
	assert.True(t, flipped, "the CAS must succeed once the concurrent lock releases")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}

// TestFinding2LockContention_CancelIfPastDueOccurrenceWithRetention_ClockTimestampReflectsWallTimeAcrossLockWait
// covers CancelIfPastDueOccurrenceWithRetention's ROUND 23 restructure — the occurrence-scoped CAS
// sibling, whose predicate also gates on `past_due_started_at = expectedPastDueStartedAt`.
func TestFinding2LockContention_CancelIfPastDueOccurrenceWithRetention_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_pastdueocc_" + suffix,
		Email:              "f2-pastdueocc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	pastDueStarted := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueStarted))

	started := time.Now()
	expires := started.AddDate(0, 6, 0)
	var flipped bool
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		var innerErr error
		flipped, innerErr = repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, pastDueStarted, started, expires)
		return innerErr
	})
	require.NoError(t, callErr)
	assert.True(t, flipped, "the occurrence-scoped CAS must succeed once the concurrent lock releases, for the matching occurrence")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}

// TestFinding2LockContention_ExpireTrialIfNoOverride_ClockTimestampReflectsWallTimeAcrossLockWait
// covers ExpireTrialIfNoOverride's ROUND 23 restructure — the third genuine CAS writer.
func TestFinding2LockContention_ExpireTrialIfNoOverride_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_trialexp_" + suffix,
		Email:              "f2-trialexp-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	var flipped bool
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		var innerErr error
		flipped, innerErr = repo.ExpireTrialIfNoOverride(ctx, user.ID)
		return innerErr
	})
	require.NoError(t, callErr)
	assert.True(t, flipped, "the CAS must succeed once the concurrent lock releases")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}
