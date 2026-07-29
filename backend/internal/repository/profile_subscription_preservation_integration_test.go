//go:build integration

package repository

// Integration tests for UpdateSubscriptionFromPaddle nil=preserve semantics.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestUpdateSubscriptionFromPaddle_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestUpdateSubscriptionFromPaddle_Integration_PreservesIDWhenNil verifies that when
// subscriptionID is nil, UpdateSubscriptionFromPaddle leaves the existing
// paddle_subscription_id and paddle_subscription_id_blind_idx values untouched.
// pgxmock cannot prove this (it only captures query text); a real DB is required.
// #933: nil = preserve; fixes the cancel-path ID-clearing bug and the past_due
// reconciliation-eligibility side effect (blind_idx must stay non-NULL).
func TestUpdateSubscriptionFromPaddle_Integration_PreservesIDWhenNil(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create a user with a known subscription ID.
	user := &domain.User{
		ClerkUserID:        "clerk_sub_preserve_" + suffix,
		Email:              "sub-preserve-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	initialSubID := "sub_preserve_" + uuid.New().String()

	// Set an initial subscription ID.
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, &initialSubID)
	require.NoError(t, err, "initial UpdateSubscriptionFromPaddle with non-nil ID should succeed")

	// Verify the blind index is set (required for reconciliation eligibility).
	var blindIdxBefore *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxBefore)
	require.NoError(t, err)
	require.NotNil(t, blindIdxBefore, "blind_idx should be non-NULL after setting subscription ID")

	// Call with nil subscriptionID (e.g., cancel path) — should preserve both columns.
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil)
	require.NoError(t, err, "UpdateSubscriptionFromPaddle with nil subscriptionID should succeed")

	// Assert: paddle_subscription_id_blind_idx still non-NULL (reconciliation-eligibility guard).
	var blindIdxAfter *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxAfter)
	require.NoError(t, err)
	assert.NotNil(t, blindIdxAfter, "blind_idx must be preserved (not NULLed) when subscriptionID is nil")
	assert.Equal(t, *blindIdxBefore, *blindIdxAfter, "blind_idx value must be unchanged after nil-subscriptionID update")

	// Verify paddle_subscription_id is also preserved (not NULL).
	var encryptedIDAfter *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id FROM users WHERE id = $1", user.ID).Scan(&encryptedIDAfter)
	require.NoError(t, err)
	assert.NotNil(t, encryptedIDAfter, "paddle_subscription_id must be preserved (not NULLed) when subscriptionID is nil")
}

// TestUpdateSubscriptionFromPaddle_Integration_PreservesBlindIdxForPastDue verifies the
// reconciliation-eligibility invariant (#933): when a past_due webhook calls
// UpdateSubscriptionFromPaddle with subscriptionID=nil, paddle_subscription_id_blind_idx
// must remain non-NULL so the user stays eligible for reconciliation
// (profile_repository.go uses blind_idx IS NOT NULL to find active Paddle subscriptions).
func TestUpdateSubscriptionFromPaddle_Integration_PreservesBlindIdxForPastDue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d_pastdue", time.Now().UnixNano())

	user := &domain.User{
		ClerkUserID:        "clerk_sub_pastdue_" + suffix,
		Email:              "sub-pastdue-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	initialSubID := "sub_pastdue_" + uuid.New().String()

	// Set an initial subscription ID so blind_idx is non-NULL.
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, &initialSubID)
	require.NoError(t, err)

	var blindIdxBefore *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxBefore)
	require.NoError(t, err)
	require.NotNil(t, blindIdxBefore, "blind_idx should be non-NULL after setting subscription ID")

	// Simulate the past_due webhook path: nil subscriptionID, status="past_due".
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "past_due", nil, nil, nil)
	require.NoError(t, err, "UpdateSubscriptionFromPaddle with past_due + nil subscriptionID should succeed")

	// Assert: blind_idx stays non-NULL (user must remain reconciliation-eligible).
	var blindIdxAfter *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxAfter)
	require.NoError(t, err)
	assert.NotNil(t, blindIdxAfter, "blind_idx must be preserved for past_due users (reconciliation-eligibility guard)")
	assert.Equal(t, *blindIdxBefore, *blindIdxAfter, "blind_idx value must be unchanged after past_due nil-subscriptionID update")

	// Also verify subscription_status was updated to past_due.
	var statusAfter string
	err = pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&statusAfter)
	require.NoError(t, err)
	assert.Equal(t, "past_due", statusAfter, "subscription_status should reflect past_due")
}

// TestUpdateSubscriptionFromPaddle_Integration_OverwritesIDWhenNonNil verifies that when
// subscriptionID is non-nil, both paddle_subscription_id and paddle_subscription_id_blind_idx
// are updated to the new values.
// #933: confirms the non-nil path still works correctly after the dynamic SET-clause change.
func TestUpdateSubscriptionFromPaddle_Integration_OverwritesIDWhenNonNil(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	user := &domain.User{
		ClerkUserID:        "clerk_sub_overwrite_" + suffix,
		Email:              "sub-overwrite-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	firstSubID := "sub_first_" + uuid.New().String()
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, &firstSubID)
	require.NoError(t, err)

	var blindIdxFirst *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxFirst)
	require.NoError(t, err)
	require.NotNil(t, blindIdxFirst)

	// Now update with a different subscription ID.
	secondSubID := "sub_second_" + uuid.New().String()
	err = repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, &secondSubID)
	require.NoError(t, err)

	var blindIdxSecond *string
	err = pool.QueryRow(ctx, "SELECT paddle_subscription_id_blind_idx FROM users WHERE id = $1", user.ID).Scan(&blindIdxSecond)
	require.NoError(t, err)
	require.NotNil(t, blindIdxSecond)
	assert.NotEqual(t, *blindIdxFirst, *blindIdxSecond, "blind_idx should change when a new subscriptionID is provided")
}

// TestUpdateSubscriptionFromPaddle_Integration_PlaidCleanupPendingAt_SetOnCancelledClearedOnActive
// proves Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with the SAME UPDATE
// that writes subscription_status='cancelled' (COALESCE-guarded — a second cancelled write must
// not push the anchor forward), and clears atomically when a later call writes an active status
// (the "resubscribe" case) — all via this single low-level primitive, so every webhook handler
// that calls it (created/activated/updated/canceled) gets correct marker semantics automatically.
func TestUpdateSubscriptionFromPaddle_Integration_PlaidCleanupPendingAt_SetOnCancelledClearedOnActive(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_sub_marker_" + suffix,
		Email:              "sub-marker-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	queryMarker := func() *time.Time {
		var m *time.Time
		require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&m))
		return m
	}

	// Transition into cancelled: marker must be set.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	firstMarker := queryMarker()
	require.NotNil(t, firstMarker, "plaid_cleanup_pending_at must be set when status transitions to cancelled")

	// A second write reporting 'cancelled' again (e.g. a redelivered webhook) must NOT push the
	// marker forward — COALESCE preserves the original timestamp.
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	secondMarker := queryMarker()
	require.NotNil(t, secondMarker)
	assert.True(t, firstMarker.Equal(*secondMarker), "a repeated cancelled write must not reset plaid_cleanup_pending_at")

	// Resubscribe (status becomes active): marker must be cleared.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))
	assert.Nil(t, queryMarker(), "plaid_cleanup_pending_at must be cleared once the user becomes active again")
}
