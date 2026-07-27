//go:build integration

package repository

// Integration tests for ExpireTrialIfNoOverride — the CAS-guarded trial-expiry transition
// (Issue #1413 High finding H5, Codex review round 3). pgxmock cannot validate the WHERE
// predicate or RowsAffected semantics this method depends on, so a real DB is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestExpireTrialIfNoOverride_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestExpireTrialIfNoOverride_Integration_Success proves the happy path: a genuinely-expired
// trial user with no override flips to inactive.
func TestExpireTrialIfNoOverride_Integration_Success(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_trial_ok_" + suffix,
		Email:              "trial-ok-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	claimed, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "an eligible trial user (no override) must be claimable")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "inactive", status, "subscription_status must flip to inactive")
}

// TestExpireTrialIfNoOverride_Integration_ActiveOverride_NoFlip proves Issue #1413 High finding
// H5: the actual interleaving — an admin grants a comp on a trial user, THEN the expiry job
// attempts the CAS flip — must not flip the status, unlike a pre-fix unconditional
// UpdateSubscriptionStatus would have.
func TestExpireTrialIfNoOverride_Integration_ActiveOverride_NoFlip(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_trial_comped_" + suffix,
		Email:              "trial-comped-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "the claim must fail for a trial user with a live manual tier override")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "trial", status, "subscription_status must remain trial when a comp is live")
}

// TestExpireTrialIfNoOverride_Integration_ExpiredOverride_StillFlips proves the negative
// direction: a lapsed override must not block the flip — otherwise a formerly-comped user's
// trial would never expire.
func TestExpireTrialIfNoOverride_Integration_ExpiredOverride_StillFlips(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_trial_expired_" + suffix,
		Email:              "trial-expired-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
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

	claimed, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "the claim must still succeed once the override has lapsed")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "inactive", status)
}

// TestExpireTrialIfNoOverride_Integration_NonTrialStatus_NoFlip proves the predicate also
// requires subscription_status = 'trial' — a user who already converted (e.g. to active) must
// not be touched.
func TestExpireTrialIfNoOverride_Integration_NonTrialStatus_NoFlip(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_trial_converted_" + suffix,
		Email:              "trial-converted-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active", // already converted, no longer trial
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	claimed, err := repo.ExpireTrialIfNoOverride(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "a non-trial user must not be claimed")

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT subscription_status FROM users WHERE id = $1", user.ID).Scan(&status))
	assert.Equal(t, "active", status)
}
