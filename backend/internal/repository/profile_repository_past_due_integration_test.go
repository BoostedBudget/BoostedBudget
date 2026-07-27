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

// TestCancelIfPastDue_Integration verifies the CAS flip: only flips past_due→cancelled once;
// a second call (simulating a concurrent recovery or re-run) returns (false, nil).
// pgxmock cannot validate the WHERE predicate or RowsAffected semantics — a real DB is required.
func TestCancelIfPastDue_Integration_CASSemantics(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	user := &domain.User{
		ClerkUserID:        "clerk_cas_" + suffix,
		Email:              "cas-" + suffix + "@example.com",
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

	// First call: user is past_due → should flip to cancelled and return true.
	flipped, err := repo.CancelIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, flipped, "CancelIfPastDue should return true when user is past_due")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "cancelled", status, "subscription_status should be cancelled after CAS flip")

	// Second call: user is now cancelled → CAS predicate misses → should return false without error.
	flipped2, err := repo.CancelIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, flipped2, "CancelIfPastDue should return false when user is already cancelled (idempotent)")

	// Status must still be cancelled — not double-updated.
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "cancelled", status, "subscription_status must remain cancelled after second CAS call")
}

// TestCancelIfPastDue_Integration_SkipsNonPastDue verifies that active/inactive users are
// never flipped — only past_due rows match the WHERE predicate.
func TestCancelIfPastDue_Integration_SkipsNonPastDue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	for _, status := range []string{"active", "cancelled", "inactive"} {
		s := status
		t.Run(s, func(t *testing.T) {
			u := &domain.User{
				ClerkUserID:        "clerk_cas_" + s + "_" + suffix,
				Email:              "cas-" + s + "-" + suffix + "@example.com",
				SubscriptionTier:   "premium",
				SubscriptionStatus: s,
				PrimaryRegion:      "iad",
				IsActive:           true,
			}
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			require.NoError(t, repo.CreateUserTx(ctx, tx, u))
			require.NoError(t, tx.Commit(ctx))
			t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID) }) //nolint:errcheck

			flipped, err := repo.CancelIfPastDue(ctx, u.ID)
			require.NoError(t, err)
			assert.False(t, flipped, "CancelIfPastDue must not flip a %s user", s)

			var got string
			require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", u.ID).Scan(&got))
			assert.Equal(t, s, got, "subscription_status must be unchanged for a %s user", s)
		})
	}
}

// TestCancelIfPastDue_Integration_ActiveOverride_NoFlip proves Issue #1413 High finding H6
// (Codex review round 3): CancelIfPastDue's predicate was extended to also require no live
// manual tier override. This performs the actual interleaving the design note calls for — grant
// a comp on a past_due user, THEN attempt the CAS flip — rather than merely testing the happy
// path. pgxmock cannot validate this: it only matches query text, not the live row state the
// added predicate now depends on.
func TestCancelIfPastDue_Integration_ActiveOverride_NoFlip(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_cas_override_" + suffix,
		Email:              "cas-override-" + suffix + "@example.com",
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

	// Simulates an admin granting a comp AFTER the worker read the user as eligible but BEFORE
	// the CAS flip below runs — the exact race window H6 closes.
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	flipped, err := repo.CancelIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, flipped, "CancelIfPastDue must not flip a user with a live manual tier override")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "past_due", status, "subscription_status must remain past_due when a comp is live")
}

// TestCancelIfPastDue_Integration_ExpiredOverride_StillFlips proves the negative direction: an
// override that has already LAPSED must not block the flip — an over-tight predicate that
// treats any override (live or expired) as a block would silently stop cancellation forever for
// a user who was comped in the past. Mirrors HasActiveManualOverride()'s boundary semantics.
func TestCancelIfPastDue_Integration_ExpiredOverride_StillFlips(t *testing.T) {
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
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	flipped, err := repo.CancelIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, flipped, "CancelIfPastDue must still flip a user whose override has already expired")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "cancelled", status)
}

// TestClaimPlaidDisconnectIfPastDue_Integration_Success proves the happy path: a genuinely
// eligible past_due user (no override) can claim the day-14 Plaid disconnect.
func TestClaimPlaidDisconnectIfPastDue_Integration_Success(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pastDueStarted := time.Now().Add(-15 * 24 * time.Hour)
	user := &domain.User{
		ClerkUserID:        "clerk_claim_ok_" + suffix,
		Email:              "claim-ok-" + suffix + "@example.com",
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

	_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1 WHERE id = $2`, pastDueStarted, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimPlaidDisconnectIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "an eligible past_due user (no override) must be able to claim the disconnect")

	// The claim self-assigns past_due_started_at — it must be unchanged, not nulled or reset.
	var gotPastDueStartedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT past_due_started_at FROM users WHERE id = $1", user.ID).Scan(&gotPastDueStartedAt))
	assert.WithinDuration(t, pastDueStarted, gotPastDueStartedAt, time.Microsecond,
		"the claim's self-assignment must not alter past_due_started_at")
}

// TestClaimPlaidDisconnectIfPastDue_Integration_ActiveOverride_NoClaim proves Issue #1413 High
// finding H6: an admin comp granted on a past_due user must block the claim — the actual race
// interleaving, not just a happy-path assertion.
func TestClaimPlaidDisconnectIfPastDue_Integration_ActiveOverride_NoClaim(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pastDueStarted := time.Now().Add(-15 * 24 * time.Hour)
	user := &domain.User{
		ClerkUserID:        "clerk_claim_comped_" + suffix,
		Email:              "claim-comped-" + suffix + "@example.com",
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

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1, manual_tier_override = $2, manual_tier_ends_at = $3 WHERE id = $4`,
		pastDueStarted, "household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimPlaidDisconnectIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "the claim must fail for a user with a live manual tier override")
}

// TestClaimPlaidDisconnectIfPastDue_Integration_NonPastDue_NoClaim proves the claim also
// requires subscription_status = 'past_due' — a user who recovered must not be claimed.
func TestClaimPlaidDisconnectIfPastDue_Integration_NonPastDue_NoClaim(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pastDueStarted := time.Now().Add(-15 * 24 * time.Hour)
	user := &domain.User{
		ClerkUserID:        "clerk_claim_recovered_" + suffix,
		Email:              "claim-recovered-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active", // recovered
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	_, err = pool.Exec(ctx, `UPDATE users SET past_due_started_at = $1 WHERE id = $2`, pastDueStarted, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimPlaidDisconnectIfPastDue(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "the claim must fail once the user has recovered to a non-past_due status")
}
