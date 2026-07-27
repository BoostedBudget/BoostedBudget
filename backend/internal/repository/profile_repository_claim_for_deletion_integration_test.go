//go:build integration

package repository

// Integration tests for ClaimForDeletion (Issue #1413 Critical finding C2, Codex review round 2).
//
// ExecuteDeletion previously read a user, checked HasActiveManualOverride() &&
// DeletionRequestedAt == nil against that snapshot, then performed irreversible external
// cleanup (Paddle cancel, Plaid disconnect) and a cascade delete — nothing tied the final
// deletion to the state that was checked. ClaimForDeletion closes that race: the predicate that
// authorises the destructive work now lives inside a conditional UPDATE, evaluated against
// committed state at the moment of the claim, not an earlier, non-atomic read. pgxmock cannot
// validate this — it only matches query text, not the live row state the predicate depends on
// or RowsAffected semantics — so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestClaimForDeletion_Integration -tags integration -v

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

// createClaimForDeletionFixtureUser inserts a plain user row, then sets is_deleted,
// deletion_requested_at, manual_tier_override, and manual_tier_ends_at directly via UPDATE (the
// same pattern used throughout this package's manual-override fixtures), and registers cleanup.
func createClaimForDeletionFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	isDeleted bool,
	deletionRequestedAt *time.Time,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_claimdel_" + suffix,
		Email:              "claimdel-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET is_deleted = $1,
		    deletion_requested_at = $2,
		    manual_tier_override = $3,
		    manual_tier_ends_at = $4
		WHERE id = $5`,
		isDeleted, deletionRequestedAt, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	user.IsDeleted = isDeleted
	user.DeletionRequestedAt = deletionRequestedAt
	user.ManualTierOverride = manualTierOverride
	user.ManualTierEndsAt = manualTierEndsAt
	return user
}

// TestClaimForDeletion_Integration_NonComped_Claims proves the happy path: a genuinely eligible
// user (no override at all, no deletion request) can claim deletion. Also verifies the claim
// itself is non-destructive — it must not flip is_deleted; that remains DeleteUserCascade's job.
func TestClaimForDeletion_Integration_NonComped_Claims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "an ordinary never-comped user with no deletion request must be claimable")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted, "ClaimForDeletion is a reservation, not the deletion itself — is_deleted must remain FALSE")
}

// TestClaimForDeletion_Integration_ActiveOverride_NotClaimed is the race-closing proof: this
// performs the actual interleaving the design note calls for — grant a comp on a candidate row
// (simulating an admin acting between the retention worker's candidate selection and this call),
// THEN attempt the claim — rather than merely testing the happy path.
func TestClaimForDeletion_Integration_ActiveOverride_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	// Simulates an admin granting a comp AFTER the retention worker selected this user as a
	// candidate but BEFORE ExecuteDeletion's claim runs — the exact race window C2 closes.
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	tier := "premium"
	_, err := pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		tier, overrideEndsAt, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "a user with a live manual tier override and no deletion request must NOT be claimed")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted)
}

// TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims is the
// ratified-rule proof the brief requires explicitly: a user's own GDPR deletion request beats
// an admin comp. A comped user who has ALSO asked to be deleted must still be claimed — a
// blanket comp check (rather than one conditional on DeletionRequestedAt) would illegally
// strand this user.
func TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "a user's own outstanding GDPR deletion request must beat a live admin comp")
}

// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_StillClaims proves the
// negative direction: an override that has already LAPSED must not block the claim — the
// point-in-time transition uses noActiveManualOverridePredicate ("still live right now"), the
// same rule CancelIfPastDue/ExpireTrialIfNoOverride use, not the retention queries' "any
// override at all" rule. An over-tight predicate that treats any override (live or expired) as
// a block would silently strand a user who was comped in the past forever.
func TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_StillClaims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "a user whose override has already expired must still be claimable")
}

// TestClaimForDeletion_Integration_AlreadyDeleted_NotClaimed proves is_deleted = TRUE is
// excluded defense-in-depth (ExecuteDeletion already short-circuits on IsDeleted before ever
// calling ClaimForDeletion, but the claim itself must be safe to call standalone too).
func TestClaimForDeletion_Integration_AlreadyDeleted_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, true, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "an already-deleted user must not be claimable again")
}

// TestClaimForDeletion_Integration_MissingUser_NotClaimed proves a non-existent user ID simply
// fails the claim (0 rows affected) rather than erroring.
func TestClaimForDeletion_Integration_MissingUser_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	claimed, err := repo.ClaimForDeletion(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.False(t, claimed)
}
