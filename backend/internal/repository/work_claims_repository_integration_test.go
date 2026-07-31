//go:build integration

package repository

// Integration tests for WorkClaimsRepository (migrations 000114/000115, issue #1413).
//
// These are the tests that matter: pgxmock can only match query text/args and
// cannot validate ON CONFLICT ... WHERE semantics or the AND lease_token = $n
// fencing predicate, so both the exclusivity guarantee and the fencing-token
// guarantee this table exists to provide are only provable against a real
// Postgres planner and its row-level locking during INSERT ... ON CONFLICT.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestWorkClaims -tags integration -v

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestResource creates a unique resourceID and registers a cleanup to delete
// the corresponding work_claims rows after the test.
func setupTestResource(t *testing.T, db DB) string {
	resourceID := uuid.New().String()
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2`,
			ResourceTypeUser, resourceID)
	})
	return resourceID
}

func TestWorkClaims_Claim_ExclusiveSequential(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	firstToken, first, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, first, "first claim on a fresh (resource, action) must win")
	assert.NotEmpty(t, firstToken, "the winning claim must return a non-empty fencing token")

	secondToken, second, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, second, "second claim while the first lease is unexpired must lose")
	assert.Empty(t, secondToken, "a losing claim must return an empty token")
}

func TestWorkClaims_Claim_ExpiredLeaseIsReclaimable(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	firstToken, first, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, first, "first claim must win")
	require.NotEmpty(t, firstToken)

	// Simulate a crashed worker: nobody calls Complete or Release, and enough
	// wall-clock time has passed that the lease has expired. Push the lease into
	// the past directly via SQL rather than sleeping out a real lease — sleep-based
	// timing assertions are flaky under CI load; this is deterministic.
	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionAccountDeletion)
	require.NoError(t, err)

	reclaimToken, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "an expired lease from a crashed worker must not block forever")
	assert.NotEmpty(t, reclaimToken)
	assert.NotEqual(t, firstToken, reclaimToken, "a reclaim after lease expiry must issue a DIFFERENT fencing token than the original")
}

func TestWorkClaims_Complete_NeverReGranted(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	ok, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d, token)
	require.NoError(t, err)
	require.True(t, ok)

	// Deterministic stand-in for "the lease would eventually expire too" — a
	// completed claim must never be re-granted regardless of lease state.
	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionRetentionNotice30d)
	require.NoError(t, err)

	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice30d, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed, "a completed claim must never be re-granted, lease expiry notwithstanding")
}

// TestWorkClaims_Release_DoesNotResurrectCompletedClaim is the regression test for a
// defect an adversarial review surfaced: Release originally unconditionally cleared
// completed_at, so a stale/late Release call (e.g. from a worker that lost its lease
// and doesn't know its work already succeeded via another path) could un-complete a
// claim that must never be re-granted. Release must now be a no-op against an
// already-completed claim, even when presented with the (still valid, matching)
// token — the completed_at guard is defense-in-depth independent of fencing.
func TestWorkClaims_Release_DoesNotResurrectCompletedClaim(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	completeOk, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, token)
	require.NoError(t, err)
	require.True(t, completeOk)

	// A stray Release call after completion, even with the correct token, must be
	// a harmless no-op (ok=false), not an error and not a resurrection.
	releaseOk, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, token, "stale worker reporting failure")
	require.NoError(t, err)
	assert.False(t, releaseOk, "Release against an already-completed claim must report ok=false")

	var completedAt *time.Time
	err = pool.QueryRow(ctx,
		`SELECT completed_at FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionPastDueGraceCancel,
	).Scan(&completedAt)
	require.NoError(t, err)
	assert.NotNil(t, completedAt, "Release must not resurrect a completed claim by clearing completed_at")

	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed, "a completed claim must remain un-reclaimable even after a stray Release call")
}

// TestWorkClaims_Complete_ReturnsNoMatchWhenNeverClaimed proves Complete reports
// ok=false (with no error) for a resource/action/token tuple that has no
// matching row — e.g. a mismatched action from a typo. Complete no longer
// errors here: "no matching row" is now the same signal used for a stale
// fencing token, and both cases share the same caller-side response (don't
// treat the work as recorded).
func TestWorkClaims_Complete_ReturnsNoMatchWhenNeverClaimed(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	ok, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, uuid.New().String())
	require.NoError(t, err, "Complete on a resource/action that was never Claimed must not error")
	assert.False(t, ok, "Complete on a resource/action that was never Claimed must report ok=false")
}

func TestWorkClaims_Release_ImmediatelyReclaimableAndRecordsReason(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	const reason = "plaid disconnect call failed: 503"
	ok, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, token, reason)
	require.NoError(t, err)
	assert.True(t, ok, "Release with the current token must report ok=true")

	// Check last_error was recorded BEFORE reclaiming — a subsequent successful
	// Claim resets last_error to NULL, so order matters here.
	var lastError string
	err = pool.QueryRow(ctx,
		`SELECT last_error FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionPastDueGraceCancel,
	).Scan(&lastError)
	require.NoError(t, err)
	assert.Equal(t, reason, lastError, "Release must record why the work failed")

	// Immediately reclaimable — no need to wait out the (long, 15m) lease.
	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "Release must make the claim immediately reclaimable, not wait for lease expiry")
}

func TestWorkClaims_Release_RecordsReason_ObservedBeforeReclaim(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	const reason = "paddle cleanup call timed out"
	ok, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, token, reason)
	require.NoError(t, err)
	assert.True(t, ok)

	var (
		lastError      string
		leaseExpiresAt time.Time
		completedAt    *time.Time
	)
	err = pool.QueryRow(ctx,
		`SELECT last_error, lease_expires_at, completed_at FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
	).Scan(&lastError, &leaseExpiresAt, &completedAt)
	require.NoError(t, err)

	assert.Equal(t, reason, lastError, "Release must record the failure reason")
	assert.Nil(t, completedAt, "Release must not mark the claim completed")
	assert.True(t, leaseExpiresAt.Before(time.Now()), "Release must leave the lease already expired")
}

func TestWorkClaims_Complete_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	ok1, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, token)
	require.NoError(t, err)
	require.True(t, ok1)

	ok2, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, token)
	require.NoError(t, err)
	assert.True(t, ok2, "Complete must be idempotent when called twice with the same valid token")

	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed)
}

func TestWorkClaims_Release_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	ok1, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, token, "first failure")
	require.NoError(t, err)
	assert.True(t, ok1)

	ok2, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, token, "second failure")
	require.NoError(t, err)
	assert.True(t, ok2, "Release must be idempotent (with the same valid token)")

	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, reclaimed, "still reclaimable after two Releases")
}

// TestWorkClaims_Complete_StaleTokenFails_LeavesCurrentLeaseUntouched proves the
// fencing check on Complete: a caller presenting a token that is no longer the
// row's current lease_token must get ok=false, and — critically — must not
// perturb the row the CURRENT (different) token holder owns. This is the "Complete
// with a stale token" case from the #1413 fencing-token requirements.
func TestWorkClaims_Complete_StaleTokenFails_LeavesCurrentLeaseUntouched(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	staleToken, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	// Force the lease to expire and reclaim it, so the row's current lease_token
	// is now different from staleToken — simulating a worker whose external work
	// outlived its lease while another worker legitimately took over.
	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionAccountDeletion)
	require.NoError(t, err)

	currentToken, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, reclaimed)
	require.NotEqual(t, staleToken, currentToken)

	// Snapshot the current lease row before the stale Complete call.
	var beforeLeaseExpiresAt time.Time
	var beforeCompletedAt *time.Time
	var beforeAttempts int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT lease_expires_at, completed_at, attempts FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionAccountDeletion,
	).Scan(&beforeLeaseExpiresAt, &beforeCompletedAt, &beforeAttempts))

	ok, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, staleToken)
	require.NoError(t, err)
	assert.False(t, ok, "Complete with a stale token must report ok=false")

	var afterLeaseExpiresAt time.Time
	var afterCompletedAt *time.Time
	var afterAttempts int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT lease_expires_at, completed_at, attempts FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionAccountDeletion,
	).Scan(&afterLeaseExpiresAt, &afterCompletedAt, &afterAttempts))

	assert.Nil(t, afterCompletedAt, "stale Complete must not mark the reclaimer's lease completed")
	assert.Equal(t, beforeLeaseExpiresAt, afterLeaseExpiresAt, "stale Complete must not touch the reclaimer's lease_expires_at")
	assert.Equal(t, beforeAttempts, afterAttempts, "stale Complete must not touch attempts")

	// The reclaimer's own token must still work.
	ok, err = repo.Complete(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, currentToken)
	require.NoError(t, err)
	assert.True(t, ok, "the current token holder must still be able to Complete")
}

// TestWorkClaims_Release_StaleTokenFails_LeavesReclaimersLeaseUntouched proves the
// fencing check on Release: a stale token must not disturb the reclaimer's
// in-progress lease or last_error. This is the "Release with a stale token" case
// from the #1413 fencing-token requirements.
func TestWorkClaims_Release_StaleTokenFails_LeavesReclaimersLeaseUntouched(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	staleToken, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup)
	require.NoError(t, err)

	currentToken, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, reclaimed)
	require.NotEqual(t, staleToken, currentToken)

	var beforeLeaseExpiresAt time.Time
	var beforeLastError *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT lease_expires_at, last_error FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
	).Scan(&beforeLeaseExpiresAt, &beforeLastError))
	assert.Nil(t, beforeLastError, "last_error must have been reset to NULL by the reclaiming Claim")

	ok, err := repo.Release(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, staleToken, "stale worker's failure report")
	require.NoError(t, err)
	assert.False(t, ok, "Release with a stale token must report ok=false")

	var afterLeaseExpiresAt time.Time
	var afterLastError *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT lease_expires_at, last_error FROM work_claims
		 WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
	).Scan(&afterLeaseExpiresAt, &afterLastError))

	assert.Equal(t, beforeLeaseExpiresAt, afterLeaseExpiresAt, "stale Release must not touch the reclaimer's lease_expires_at")
	assert.Nil(t, afterLastError, "stale Release must not write last_error onto the reclaimer's lease")

	// The reclaimer's own token must still work.
	ok, err = repo.Release(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, currentToken, "reclaimer's own failure")
	require.NoError(t, err)
	assert.True(t, ok, "the current token holder must still be able to Release")
}

// TestWorkClaims_StaleWorkerScenario_EndToEnd is the scenario this whole fencing
// token exists to fix: A claims, A's lease expires (not crashed — just slow),
// B legitimately reclaims and does the same external work, and THEN A's
// Complete call comes in. Without the fencing token, A's Complete would mark
// B's lease finished — the record would show one execution when two happened.
// With it, A's Complete must fail loudly (ok=false) and B's Complete must
// still succeed cleanly afterward.
func TestWorkClaims_StaleWorkerScenario_EndToEnd(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	// A claims.
	tokenA, claimedA, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimedA)
	require.NotEmpty(t, tokenA)

	// A's lease expires while A is still doing (slow, not crashed) external work.
	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue)
	require.NoError(t, err)

	// B reclaims and starts the same external work.
	tokenB, claimedB, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimedB)
	require.NotEmpty(t, tokenB)
	require.NotEqual(t, tokenA, tokenB, "B's reclaim must issue a different token than A's original")

	// A, unaware it lost the lease, finally finishes its (duplicate) external
	// work and calls Complete with its now-stale token.
	okA, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, tokenA)
	require.NoError(t, err)
	assert.False(t, okA, "A's Complete with a stale token must report ok=false")

	// A's stale Complete must NOT have completed B's lease.
	var completedAtAfterA *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT completed_at FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue,
	).Scan(&completedAtAfterA))
	assert.Nil(t, completedAtAfterA, "A's stale Complete must not mark B's lease finished")

	// B completes its own work with its own (current) token, and it must succeed.
	okB, err := repo.Complete(ctx, ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue, tokenB)
	require.NoError(t, err)
	assert.True(t, okB, "B's Complete with its current token must succeed")

	var completedAtAfterB *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT completed_at FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
		ResourceTypeUser, resourceID, ActionPlaidDisconnectPastDue,
	).Scan(&completedAtAfterB))
	assert.NotNil(t, completedAtAfterB, "B's Complete must durably record completion")
}

// TestWorkClaims_Claim_ConcurrentRace is the falsifiability-critical test: it proves
// exclusivity under genuine concurrency against the live DB, not just sequential calls
// that could theoretically pass due to test ordering rather than real locking semantics.
//
// A sync.WaitGroup barrier holds every goroutine at the starting line until all of
// them have been scheduled and are blocked on <-start, so closing start releases all
// workers at once rather than however many happened to reach the receive by the time
// the (unsynchronized) main goroutine closed it. Every result's error is asserted
// nil — a test that silently maps DB errors to "not claimed" could report exactly
// one winner even if the other 24 calls failed outright rather than lost fairly.
//
// It also asserts the fencing-token contract under concurrency: the single winner
// must get a non-empty token, and every loser must get an empty one — a claim
// implementation that occasionally leaked a token to a losing caller would let
// that caller later Complete/Release someone else's lease.
func TestWorkClaims_Claim_ConcurrentRace(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool)

	const workers = 25
	type claimResult struct {
		token   string
		claimed bool
		err     error
	}
	results := make(chan claimResult, workers)

	var ready sync.WaitGroup
	ready.Add(workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			ready.Done()
			<-start // every goroutine waits here until all `workers` have arrived
			token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionAccountDeletion, 15*time.Minute)
			results <- claimResult{token: token, claimed: claimed, err: err}
		}()
	}
	ready.Wait() // block until every goroutine is parked on <-start
	close(start)

	winners := 0
	var winnerToken string
	for i := 0; i < workers; i++ {
		r := <-results
		require.NoError(t, r.err, "a concurrent Claim call must not error")
		if r.claimed {
			winners++
			winnerToken = r.token
			assert.NotEmpty(t, r.token, "the winning claim must have a non-empty token")
		} else {
			assert.Empty(t, r.token, "a losing claim must have an empty token")
		}
	}

	assert.Equal(t, 1, winners, "exactly one of %d concurrent Claim calls for the same (resource, action) must win", workers)
	assert.NotEmpty(t, winnerToken, "the single winner's token must be non-empty")
}

// TestWorkClaims_Claim_GatesExternalWork_WonVsLost is the corrected replacement for
// TestWorkClaims_Claim_EligibleResourceIsProcessedEndToEnd (Issue #1413 round-3 finding
// M2, "the ninth test in this branch found not testing what it claims"). The original
// test's "external work" was the literal comment `// ... external work would happen
// here ...` — no code ran there at all — so it only ever exercised Claim followed
// unconditionally by Complete. It would have passed even if production code invoked its
// side effect (a Plaid/Paddle call) WITHOUT ever checking whether the claim was won,
// which is exactly the bug class this whole mechanism exists to prevent.
//
// This test asserts the GATING itself, not just the claim: a spy collaborator standing
// in for the real external side effect is called exactly once when the claim is won, and
// is never called when the claim is lost — for both ways a claim can be lost (already
// completed, and held by another worker's unexpired lease). doGatedWork mirrors the
// #1413 design doc's required call shape (claim -> call collaborator ONLY if claimed ->
// Complete), which is what a real caller like DeletionService.ExecuteDeletion does.
func TestWorkClaims_Claim_GatesExternalWork_WonVsLost(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewWorkClaimsRepository(pool)

	doGatedWork := func(action, resourceID string, spy func()) (claimed bool) {
		token, won, err := repo.Claim(ctx, ResourceTypeUser, resourceID, action, 15*time.Minute)
		require.NoError(t, err)
		if !won {
			return false
		}
		spy()
		ok, err := repo.Complete(ctx, ResourceTypeUser, resourceID, action, token)
		require.NoError(t, err)
		require.True(t, ok)
		return true
	}

	t.Run("won calls collaborator exactly once and durably completes", func(t *testing.T) {
		resourceID := setupTestResource(t, pool)

		callCount := 0
		claimed := doGatedWork(ActionSubscriptionCancelledCleanup, resourceID, func() { callCount++ })
		assert.True(t, claimed, "a fresh resource with no prior claim must be claimable")
		assert.Equal(t, 1, callCount, "the collaborator must run exactly once when the claim is won")

		var completedAt *time.Time
		err := pool.QueryRow(ctx,
			`SELECT completed_at FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup,
		).Scan(&completedAt)
		require.NoError(t, err)
		assert.NotNil(t, completedAt, "Complete must durably record completion")
	})

	t.Run("lost because already completed never calls collaborator", func(t *testing.T) {
		resourceID := setupTestResource(t, pool)

		// Win and complete once — a genuine first run.
		require.True(t, doGatedWork(ActionSubscriptionCancelledCleanup, resourceID, func() {}))

		// A second attempt against the same (resource, action) must lose (already
		// completed) and — this is the gating M2 found missing — must NOT invoke the
		// collaborator, i.e. the external side effect, again.
		callCount := 0
		claimed := doGatedWork(ActionSubscriptionCancelledCleanup, resourceID, func() { callCount++ })
		assert.False(t, claimed, "a completed claim must never be re-granted")
		assert.Equal(t, 0, callCount, "the collaborator must NOT run when the claim is lost to completion")
	})

	t.Run("lost to another worker's unexpired lease never calls collaborator", func(t *testing.T) {
		resourceID := setupTestResource(t, pool)

		_, won, err := repo.Claim(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
		require.NoError(t, err)
		require.True(t, won, "the first claim must win and hold the lease")

		callCount := 0
		claimed := doGatedWork(ActionSubscriptionCancelledCleanup, resourceID, func() { callCount++ })
		assert.False(t, claimed, "an unexpired lease held by another worker must not be re-granted")
		assert.Equal(t, 0, callCount, "the collaborator must NOT run while another worker holds the lease")
	})
}

func TestWorkClaims_Action_CheckConstraintRejectsUnknownAction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO work_claims (resource_type, resource_id, action, lease_expires_at)
		VALUES ($1, $2, 'not_a_real_action', NOW() + INTERVAL '15 minutes')
	`, ResourceTypeUser, resourceID)
	assert.Error(t, err, "the action CHECK constraint must reject free-form strings")
}

// --- ClaimOutcome (#1413 round-3 finding H3) ---
//
// These are the tests that actually PROVE the H3 fix: pgxmock can only echo back canned rows,
// so the unit tests in work_claims_repository_test.go pin down the query shape but cannot show
// that "held by another live worker" and "already completed" are genuinely distinguished by the
// live ON CONFLICT ... WHERE semantics. These do.

func TestWorkClaims_ClaimOutcome_FreshResourceWins(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool).(WorkClaimsOutcomeRepository)

	token, outcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, resourceID, ActionSubscriptionCancelledCleanup, 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, ClaimWon, outcome, "a fresh (resource, action) with no prior row must win")
	assert.NotEmpty(t, token)
}

// TestWorkClaims_ClaimOutcome_DistinguishesHeldByOtherFromAlreadyCompleted is the falsifying
// test for the bug H3 describes: an implementation that just wraps the bare bool Claim (i.e.
// always reports the same outcome for both "not claimed" cases) would fail this, because it
// would report the SAME outcome for a live in-flight lease as for a genuinely finished one.
func TestWorkClaims_ClaimOutcome_DistinguishesHeldByOtherFromAlreadyCompleted(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewWorkClaimsRepository(pool).(WorkClaimsOutcomeRepository)

	// Case 1: another worker holds an unexpired, incomplete lease right now.
	heldID := setupTestResource(t, pool)
	firstToken, firstOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, heldID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, firstOutcome)
	require.NotEmpty(t, firstToken)

	heldToken, heldOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, heldID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, ClaimHeldByOther, heldOutcome, "an unexpired, incomplete lease held by another worker must report HeldByOther, not AlreadyCompleted")
	assert.Empty(t, heldToken)

	// Case 2: the action already ran to completion for this tuple.
	completedID := setupTestResource(t, pool)
	completeToken, completeOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, completedID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, completeOutcome)
	ok, err := repo.Complete(ctx, ResourceTypeUser, completedID, ActionPastDueGraceCancel, completeToken)
	require.NoError(t, err)
	require.True(t, ok)

	doneToken, doneOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, completedID, ActionPastDueGraceCancel, 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, ClaimAlreadyCompleted, doneOutcome, "a completed claim must report AlreadyCompleted, not HeldByOther")
	assert.Empty(t, doneToken)
}

// TestWorkClaims_ClaimOutcome_ExpiredLeaseIsReclaimableAsWon proves ClaimOutcome shares Claim's
// crashed-worker recovery: an expired-but-incomplete lease is reclaimable (ClaimWon), not stuck
// reporting HeldByOther forever.
func TestWorkClaims_ClaimOutcome_ExpiredLeaseIsReclaimableAsWon(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	resourceID := setupTestResource(t, pool)
	repo := NewWorkClaimsRepository(pool).(WorkClaimsOutcomeRepository)

	firstToken, firstOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, firstOutcome)
	require.NotEmpty(t, firstToken)

	_, err = pool.Exec(ctx, `
		UPDATE work_claims SET lease_expires_at = NOW() - INTERVAL '1 hour'
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, ResourceTypeUser, resourceID, ActionRetentionNotice7d)
	require.NoError(t, err)

	reclaimToken, reclaimOutcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, resourceID, ActionRetentionNotice7d, 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, ClaimWon, reclaimOutcome, "an expired, incomplete lease must be reclaimable as ClaimWon")
	assert.NotEmpty(t, reclaimToken)
	assert.NotEqual(t, firstToken, reclaimToken, "a reclaim must issue a fresh fencing token")
}
