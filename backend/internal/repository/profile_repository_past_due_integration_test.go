//go:build integration

package repository

// Integration tests for SetPastDueStartedAt and ClearPastDueStartedAt.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestPastDueStartedAt_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestPastDueStartedAt_Integration_SetAndClear verifies the full lifecycle of
// past_due_started_at: set when NULL, idempotent on repeat, clear to NULL.
// A real DB is required because pgxmock cannot verify column types or NULL constraints.
func TestPastDueStartedAt_Integration_SetAndClear(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	user := &domain.User{
		ClerkUserID:        "clerk_pdintegration_" + suffix,
		Email:              "pdintegration-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	// Verify past_due_started_at is initially NULL.
	var initialVal *time.Time
	err = pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&initialVal)
	require.NoError(t, err)
	assert.Nil(t, initialVal, "past_due_started_at should be NULL for a freshly created user")

	// Set it.
	pastDueTs := time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond)
	err = repo.SetPastDueStartedAt(ctx, user.ID, pastDueTs)
	require.NoError(t, err, "SetPastDueStartedAt should succeed when column is NULL")

	var setVal *time.Time
	err = pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&setVal)
	require.NoError(t, err)
	require.NotNil(t, setVal, "past_due_started_at should be non-NULL after SetPastDueStartedAt")
	assert.WithinDuration(t, pastDueTs, *setVal, time.Microsecond, "stored timestamp should match input")

	// Calling Set again (idempotent: WHERE past_due_started_at IS NULL should not match).
	laterTs := time.Now().Truncate(time.Microsecond)
	err = repo.SetPastDueStartedAt(ctx, user.ID, laterTs)
	require.NoError(t, err, "SetPastDueStartedAt should be a no-op (not an error) when already set")

	var afterSecondSet *time.Time
	err = pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&afterSecondSet)
	require.NoError(t, err)
	require.NotNil(t, afterSecondSet)
	assert.WithinDuration(t, pastDueTs, *afterSecondSet, time.Microsecond,
		"past_due_started_at must not change when SetPastDueStartedAt is called a second time")

	// Clear it.
	err = repo.ClearPastDueStartedAt(ctx, user.ID)
	require.NoError(t, err, "ClearPastDueStartedAt should succeed")

	var clearedVal *time.Time
	err = pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&clearedVal)
	require.NoError(t, err)
	assert.Nil(t, clearedVal, "past_due_started_at should be NULL after ClearPastDueStartedAt")
}

// TestPastDueStartedAt_Integration_ClearNotFound verifies ErrUserNotFound
// when ClearPastDueStartedAt is called for a non-existent user.
func TestPastDueStartedAt_Integration_ClearNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	nonExistentID := "00000000-0000-0000-0000-000000000000"
	err := repo.ClearPastDueStartedAt(ctx, nonExistentID)
	assert.ErrorIs(t, err, ErrUserNotFound, "ClearPastDueStartedAt on non-existent user must return ErrUserNotFound")
}

// TestPastDueStartedAt_Integration_SetNotFoundIsNoOp verifies that SetPastDueStartedAt
// on a non-existent user returns nil (0 rows affected is treated as idempotent).
func TestPastDueStartedAt_Integration_SetNotFoundIsNoOp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	nonExistentID := "00000000-0000-0000-0000-000000000001"
	err := repo.SetPastDueStartedAt(ctx, nonExistentID, time.Now())
	assert.NoError(t, err, "SetPastDueStartedAt on non-existent user returns nil (idempotent semantics)")
}

// ClaimPlaidDisconnectIfPastDue and its three integration tests that used to live here were
// deleted in Issue #1413 round 4 (High finding H6a): the method was an atomic GUARD (a
// self-assigning UPDATE) that proved its predicate held at one instant but reserved nothing — two
// workers could both see "still past_due, no override" and both disconnect the same user's Plaid
// items. It is superseded by a real work_claims claim (repository.ActionPlaidDisconnectPastDue),
// taken in the deleted past_due_service.go's processPlaidDisconnectWindow, which re-verified the same
// still-past_due/no-override predicate via reReadStillPastDue after winning the claim. See
// work_claims_repository_integration_test.go for that mechanism's exclusivity proof.

// --- ClearPastDueStartedAtIfMatches (Issue #1413 round-4 finding H3) ---
//
// ClearPastDueStartedAt above is UNCONDITIONAL (WHERE id = $1) — correct for
// subscription_webhook.go's recovery-path callers, where Paddle telling us the subscription is no
// longer past_due IS the authorization. It is WRONG for processGraceExpiry's auto-cancel step,
// which captured a SPECIFIC dunning cycle's timestamp earlier in its own run: sequence — the CAS
// (the deleted CancelIfPastDueOccurrenceWithRetention, #1652) succeeds and the flow reaches the clear step; meanwhile the user
// recovers, then re-enters past_due, and the webhook stamps a NEW past_due_started_at
// (SetPastDueStartedAt) for the NEW cycle; the ORIGINAL flow, still holding its stale snapshot,
// then reaches the unconditional clear and erases the NEW cycle's timestamp too. Because
// GetUsersInPastDue requires past_due_started_at IS NOT NULL, that user is never selected again —
// stuck past-due forever with no dunning. ClearPastDueStartedAtIfMatches closes this with a
// compare-and-swap: only clear when past_due_started_at still equals the timestamp the caller
// actually acted on.
//
// A real DB is required because pgxmock cannot prove that a STALE expected timestamp leaves a
// genuinely NEWER value in the row untouched, or that GetUsersInPastDue still selects that user
// afterward — it can only assert the query text/RowsAffected count against a canned scripted
// response, not live row state across two different timestamps racing each other.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestClearPastDueStartedAtIfMatches_Integration -tags integration -v

// TestClearPastDueStartedAtIfMatches_Integration_MatchingTimestamp_Clears proves the happy path:
// clearing with the timestamp the caller actually acted on succeeds, exactly like the
// unconditional ClearPastDueStartedAt does today.
func TestClearPastDueStartedAtIfMatches_Integration_MatchingTimestamp_Clears(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearmatch_" + suffix,
		Email:              "clearmatch-" + suffix + "@example.com",
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

	pastDueTs := time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, repo.SetPastDueStartedAt(ctx, user.ID, pastDueTs))

	cleared, err := repo.ClearPastDueStartedAtIfMatches(ctx, user.ID, pastDueTs)
	require.NoError(t, err)
	assert.True(t, cleared, "clearing with the timestamp the caller actually acted on must succeed")

	var val *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&val))
	assert.Nil(t, val, "past_due_started_at should be NULL after a matching clear")
}

// TestClearPastDueStartedAtIfMatches_Integration_StaleTimestamp_NewerCycleSurvives is THE H3
// regression proof. Sequence under test: the caller captured an EARLIER cycle's timestamp
// (staleExpected); by the time it calls this method, the row has already moved on to a NEWER
// cycle (newerCycle) — simulating a recover -> re-enter-past-due race that completed while the
// caller's flow was still in flight. Before this fix (an unconditional clear), the newer cycle's
// timestamp would have been erased and the user would never be selected by GetUsersInPastDue
// again — this is the "stuck past-due forever with no dunning" bug the round-4 review found.
func TestClearPastDueStartedAtIfMatches_Integration_StaleTimestamp_NewerCycleSurvives(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearstale_" + suffix,
		Email:              "clearstale-" + suffix + "@example.com",
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

	staleExpected := time.Now().Add(-60 * 24 * time.Hour).Truncate(time.Microsecond) // the EARLIER cycle the caller captured
	newerCycle := time.Now().Add(-1 * time.Hour).Truncate(time.Microsecond)          // a genuinely NEWER cycle already in the row

	// Simulate the row already having moved on to the newer cycle (recover -> re-enter-past-due
	// happened while the caller's flow was in flight).
	_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1, subscription_status = 'past_due' WHERE id = $2`, newerCycle, user.ID)
	require.NoError(t, err)

	cleared, err := repo.ClearPastDueStartedAtIfMatches(ctx, user.ID, staleExpected)
	require.NoError(t, err)
	assert.False(t, cleared, "a stale expected timestamp must not clear a newer cycle's stamp")

	var val *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&val))
	require.NotNil(t, val, "the newer cycle's timestamp must survive untouched")
	assert.WithinDuration(t, newerCycle, *val, time.Microsecond)

	// The regression that matters most: GetUsersInPastDue must still select this user so a future
	// dunning run can process the newer cycle. Before the fix, the unconditional clear would have
	// nulled this out and the user would never be selected again.
	users, err := repo.GetUsersInPastDue(ctx)
	require.NoError(t, err)
	found := false
	for _, u := range users {
		if u.ID == user.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "GetUsersInPastDue must still return this user — the newer cycle must not be silently dropped from dunning")
}

// TestClearPastDueStartedAtIfMatches_Integration_NotFound_ReturnsErrUserNotFound proves the
// distinction the round-4 review specifically asked for: a genuinely nonexistent user reports
// ErrUserNotFound, distinguishable from the "timestamp moved on" case above (false, nil) — the
// caller can tell "nothing to clear because there's no such user" apart from "nothing to clear
// because a newer cycle legitimately owns this row now".
func TestClearPastDueStartedAtIfMatches_Integration_NotFound_ReturnsErrUserNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	nonExistentID := "00000000-0000-0000-0000-000000000099"
	cleared, err := repo.ClearPastDueStartedAtIfMatches(ctx, nonExistentID, time.Now())
	assert.False(t, cleared)
	assert.ErrorIs(t, err, ErrUserNotFound, "a nonexistent user must return ErrUserNotFound, distinguishable from a stale-timestamp skip")
}
