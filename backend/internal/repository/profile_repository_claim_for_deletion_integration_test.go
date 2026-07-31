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

// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed is the C1
// regression proof (Issue #1413, third round of remediation): a LAPSED-but-uncleared override
// must NOT be claimable for hard deletion, no differently than a live one.
//
// This test previously asserted the opposite — that an expired override still permits the
// claim — on the theory that claimForDeletionPredicate should use the "still live right now"
// rule (noActiveManualOverridePredicate), matching CancelIfPastDueWithRetention/ExpireTrialIfNoOverride. That
// was the C1 bug, not a documented rule: the retention *queries* that select this same user as a
// deletion candidate (GetUsersWithExpiredRetention) were correctly changed to exclude ANY
// override, live or lapsed (noManualOverrideAtAllPredicate) — see that constant's doc comment for
// why: a comp pauses the retention clock while live, and is only re-armed by the independently
// scheduled manual_tier_override_expiry_check job, which has no ordering guarantee relative to
// the retention worker. If the claim used the live-only rule while the query used the
// all-overrides rule, a user whose comp lapsed moments before the expiry job re-armed their clock
// would sail through: selected by the query (any override present, so actually excluded — wait,
// the real race is a comp granted AFTER a stale candidate was selected under the OLD "no override
// at all" query semantics from before this fix existed) and then hard-deleted by this claim
// before the expiry job ever ran. Requiring claimForDeletionPredicate to agree with the retention
// queries' all-overrides rule closes that window: the worst case is a delay of up to one expiry
// job cycle, never a wrongful deletion — see
// TestClaimForDeletion_Integration_ClearedOverride_Claims immediately below for proof this is a
// delay, not a permanent exemption.
func TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed, not yet cleared
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, claimed, "a lapsed-but-uncleared override must still block the deletion claim — "+
		"this is the C1 data-loss scenario: a lapsed comp must not slip through to a hard delete")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted)
}

// TestClaimForDeletion_Integration_ClearedOverride_Claims proves the exclusion above is a DELAY,
// not a permanent exemption: once manual_tier_override is actually cleared (simulating the daily
// manual_tier_override_expiry_check job doing its job), the same user becomes claimable again.
func TestClaimForDeletion_Integration_ClearedOverride_Claims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // lapsed
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, claimed, "sanity check: still blocked before the override is cleared")

	// Simulate the expiry job clearing the column.
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = NULL, manual_tier_ends_at = NULL WHERE id = $1`, user.ID)
	require.NoError(t, err)

	claimed, err = repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, claimed, "once the expiry job clears the override column, the user must become claimable again")
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
