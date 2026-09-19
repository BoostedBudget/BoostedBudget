//go:build integration

package repository

// Issue #1413 Finding 2 (High, Codex review, PR #1443 follow-up) lock-contention regression tests.
//
// Four writers of plaid_cleanup_pending_at — UpdateSubscriptionFromPaddle, UpdateTrialStatus,
// ExpireTrialIfNoOverride, and CancelIfPastDueOccurrenceWithRetention (the last three since
// deleted by #1652) — were restructured from a bare
// `UPDATE ... SET plaid_cleanup_pending_at = COALESCE(..., NOW())`
// into a `WITH candidate AS (SELECT ... FOR NO KEY UPDATE) UPDATE ... FROM candidate` shape, mirroring
// StampMissingPlaidCleanupMarkers's own ROUND 21/23 fix (profile_repository.go) — see that method's
// doc comment for the full empirically-established mechanism this restructure depends on: a bare
// UPDATE's SET-list (including a volatile clock_timestamp() call) is computed ONCE, against the row
// version obtained BEFORE any row-lock wait, and is NOT recomputed once the wait resolves — so
// neither a bare NOW() nor a bare clock_timestamp() substitution closes the backdating bug; only
// moving the lock acquisition into its own, earlier CTE step does.
//
// Each test below proves ONE writer call path's restructured COALESCE(plaid_cleanup_pending_at,
// clock_timestamp()) reflects real, post-lock-wait wall-clock time — using the SAME real
// synchronization barrier (waitUntilBlockedBy, admin_user_repository_remove_tier_override_integration_test.go)
// as StampMissingPlaidCleanupMarkers's own equivalent test. waitUntilBlockedBy polls
// `blockerPID = ANY(pg_blocking_pids(pid))`, which is true only for a backend genuinely, currently
// blocked on blockerPID's own held lock — not "some backend somewhere is in a lock wait" (which an
// unrelated concurrent backend could satisfy). Combined with each test's fresh, uniquely-suffixed
// row and this suite's serialized (-p 1, no t.Parallel) execution, blockerPID's lock has exactly one
// possible contender at any time: the writer under test — so this barrier does identify that
// specific backend, satisfying Issue #1413 Finding 1 (Medium, Codex review, PR #1443 second
// follow-up round). Two tests cover the one remaining writer — UpdateSubscriptionFromPaddle has
// two, covering its two independent query paths per Finding 3 (Low, same round) below. (Issue #1442
// deleted a fifth writer, CancelIfPastDueWithRetention, and its dedicated test here — it had zero
// production callers; #1652 deleted CancelIfPastDueOccurrenceWithRetention, UpdateTrialStatus and
// ExpireTrialIfNoOverride, along with their own tests, which covered the identical ROUND 23
// restructure.)
//
// Falsifiability (Issue #1413's own standing requirement, sharpened by this round's brief): each of
// these tests was verified RED against BOTH of the following, by temporarily reverting the
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
// post-wait assertion below deterministically (never flaky). Issue #1413 Finding 2 (Low, Codex
// review, PR #1443 second follow-up round): this claim used to exist ONLY as this paragraph, with no
// way to re-run it short of hand-reverting profile_repository.go — see
// profile_repository_finding2_probe_test.go (this package) for the checked-in, FINDING2_RUN_PROBES=1
// gated reproduction of exactly this falsification, which a future maintainer can re-run from source
// without reconstructing anything from prose.
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
// the "no subscriptionID" query variant (subscriptionID == nil, profile_repository.go:1003). Issue
// #1413 Finding 3 (Low, Codex review, PR #1443 second follow-up round): this method has a SECOND,
// separate query path for subscriptionID != nil (profile_repository.go:995) — despite sharing the
// same CTE + FOR NO KEY UPDATE + clock_timestamp() shape, it is a textually distinct query string a
// future edit could break independently of this one, so it is no longer "equivalent by comment
// only" — see TestFinding2LockContention_UpdateSubscriptionFromPaddle_WithSubscriptionID_ClockTimestampReflectsWallTimeAcrossLockWait
// below, which exercises it directly.
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

// TestFinding2LockContention_UpdateSubscriptionFromPaddle_WithSubscriptionID_ClockTimestampReflectsWallTimeAcrossLockWait
// covers UpdateSubscriptionFromPaddle's OTHER query variant (subscriptionID != nil,
// profile_repository.go:995) — Issue #1413 Finding 3 (Low, Codex review, PR #1443 second follow-up
// round). Before this test, only the nil-subscriptionID path was ever exercised by a real call, and
// this path's ROUND 23 correctness was asserted equivalent by doc comment only; a future change
// breaking just this branch (e.g. a hand-edit that touches only the paddle_subscription_id columns
// and slips the CTE) would have left the suite green. This test drives the identical lock-contention
// scenario through the non-nil branch directly.
func TestFinding2LockContention_UpdateSubscriptionFromPaddle_WithSubscriptionID_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_f2_paddlesub_" + suffix,
		Email:              "f2-paddlesub-" + suffix + "@example.com",
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
	subscriptionID := "sub_f2_" + suffix
	startFence, releaseFence, callErr := runWriterUnderLockContention(t, pool, user.ID, finding2PostBlockHoldDuration, func() error {
		return repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", &startedAt, nil, &subscriptionID)
	})
	require.NoError(t, callErr)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assertMarkerReflectsPostWaitTime(t, startFence, releaseFence, finding2PostBlockHoldDuration, marker)
}

