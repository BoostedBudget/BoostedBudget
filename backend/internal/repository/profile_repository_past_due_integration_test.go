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
// taken in past_due_service.go's processPlaidDisconnectWindow, which re-verifies the same
// still-past_due/no-override predicate via reReadStillPastDue after winning the claim. See
// work_claims_repository_integration_test.go for that mechanism's exclusivity proof.
