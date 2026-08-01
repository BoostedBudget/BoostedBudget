//go:build integration

package repository

// Integration test for Issue #1440 round-3 Finding 7 (Codex review, coordinator's #1440 follow-up
// brief): a lock-order deadlock between DeleteUserCascade and household_service.go's AcceptInvite.
//
// AcceptInvite locks the HOUSEHOLD first (hhRepo.LockHouseholdForUpdate, household_service.go
// step 1) and the OWNER's user row second (s.lockAndGetUser, step 4) — its own doc comment states
// this ordering as an explicit codebase-wide invariant: "No path locks a users row before a
// households row ... never a cycle." DeleteUserCascade's household-owner guard (Issue #1440
// Critical finding, round 2) violated that invariant: it locks the USER row first (the eligibility
// recheck's `FOR UPDATE`), then only touches the household row implicitly, much later, when its
// final `DELETE FROM users` cascades into households via owner_user_id's CASCADE FK. Two
// transactions acquiring the SAME two rows in OPPOSITE orders is a textbook deadlock: AcceptInvite
// holds household, wants user; DeleteUserCascade holds user, wants household (via its cascading
// DELETE). Postgres's deadlock detector eventually aborts one side (SQLSTATE 40P01) after
// deadlock_timeout (~1s) — in a GDPR-deletion-adjacent path, an aborted transaction that may retry
// into the dead-letter queue is not a theoretical concern.
//
// This test reproduces the cycle deterministically: a manual transaction (txA) mirrors
// AcceptInvite's own lock sequence exactly (household FOR UPDATE, then the owner's user row FOR
// UPDATE), interleaved with a REAL repo.DeleteUserCascade call via a synchronization barrier
// (waitUntilBlockedBy, already used by the Stamp/RemoveUserTierOverride deadlock-class tests in
// this package) so the two genuinely contend for the same two rows in the same order every run,
// not by incidental timing.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeleteUserCascade_Integration_LockOrder -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_NoDeadlockWithConcurrentAcceptInvite
// proves DeleteUserCascade's lock order no longer inverts AcceptInvite's own household-then-owner
// order. txA mirrors AcceptInvite's first two lock acquisitions exactly (household FOR UPDATE, then
// the owner's user row FOR UPDATE) while a real DeleteUserCascade call runs concurrently in a
// goroutine. If DeleteUserCascade still locked the user row before the household (the pre-fix
// order), this reproduces a genuine deadlock: txA's user-row lock attempt would either fail with
// "deadlock detected" or take ~1s (Postgres's deadlock_timeout) to resolve. With the fix
// (DeleteUserCascade locks the owned household FIRST, matching AcceptInvite), txA's user-row lock
// succeeds immediately — DeleteUserCascade is still blocked on the household, holding nothing else —
// and DeleteUserCascade completes normally once txA commits and releases both locks.
func TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_NoDeadlockWithConcurrentAcceptInvite(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "lockorder-owner-"+suffix, nil, nil, nil)

	var householdID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'Lock Order H') RETURNING id`, owner.ID,
	).Scan(&householdID))
	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`,
		householdID, owner.ID)

	// txA mirrors AcceptInvite's own lock sequence: household_repository.go's LockHouseholdForUpdate
	// query first (`SELECT owner_user_id FROM households WHERE id = $1 FOR UPDATE`).
	txA, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer txA.Rollback(ctx) //nolint:errcheck // no-op after commit below

	var lockedOwnerID string
	require.NoError(t, txA.QueryRow(ctx, `SELECT owner_user_id FROM households WHERE id = $1 FOR UPDATE`, householdID).Scan(&lockedOwnerID))
	require.Equal(t, owner.ID, lockedOwnerID)

	var txAPID int32
	require.NoError(t, txA.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&txAPID))

	type deleteResult struct {
		deleted bool
		err     error
	}
	done := make(chan deleteResult, 1)
	go func() {
		deleted, callErr := repo.DeleteUserCascade(ctx, owner.ID, owner.DeletionRequestedAt)
		done <- deleteResult{deleted, callErr}
	}()

	// Real barrier: block until DeleteUserCascade's own backend is CONFIRMED waiting on txA's
	// household lock — with the fix, this happens at the very FIRST lock DeleteUserCascade takes;
	// pre-fix, only much later, once its cascading DELETE FROM users reaches the household row.
	// Either way, once this returns, DeleteUserCascade is genuinely blocked by txA.
	waitUntilBlockedBy(t, pool, txAPID, 5*time.Second)

	// The moment of truth: txA, STILL holding the household lock, now tries to lock the SAME owner
	// user row DeleteUserCascade's own eligibility recheck needs — exactly AcceptInvite's own step 4
	// (s.lockAndGetUser on the owner, household_service.go).
	ownerLockStart := time.Now()
	var reLockedID string
	err = txA.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, owner.ID).Scan(&reLockedID)
	ownerLockElapsed := time.Since(ownerLockStart)
	require.NoError(t, err, "locking the owner's user row from txA must not fail with a deadlock — with the Finding 7 fix, DeleteUserCascade locks the household BEFORE the user row, matching AcceptInvite's own order, so txA's user-row lock is uncontended while DeleteUserCascade is still blocked on the household")
	assert.Less(t, ownerLockElapsed, 500*time.Millisecond,
		"locking the owner's row must be immediate, not the resolution of a real deadlock (Postgres's deadlock_timeout is ~1s) — a slow-but-successful lock means txA only won a genuine deadlock instead of the lock-order cycle being closed at its root; got %v", ownerLockElapsed)

	require.NoError(t, txA.Commit(ctx))

	select {
	case res := <-done:
		require.NoError(t, res.err, "DeleteUserCascade must complete successfully once txA releases the household lock — no deadlock, no aborted transaction")
		assert.True(t, res.deleted)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for DeleteUserCascade to complete after txA committed")
	}

	assertUserRowGone(t, ctx, pool, owner.ID)
}
