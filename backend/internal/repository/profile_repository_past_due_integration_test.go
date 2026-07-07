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
