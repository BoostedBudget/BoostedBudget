//go:build integration

package repository

// Integration tests for DeleteUserCascade's eligibility guard (Issue #1413 round-4 Critical
// findings C1 and C2, Codex adversarial review round 4).
//
// ClaimForDeletion genuinely re-verifies eligibility with a conditional UPDATE — but the flow
// then performs external cleanup (Paddle cancel, Plaid disconnect — potentially many seconds of
// network calls) and only THEN calls DeleteUserCascade, which used to be keyed on nothing but the
// user ID. During that external-cleanup window, an admin could grant a comp, or — the serious
// case — the user could cancel their own deletion request, and the cascade would still run,
// irreversibly destroying an account whose owner explicitly changed their mind.
//
// pgxmock cannot validate any of this — it only matches query text, not the live row state the
// FOR UPDATE lock and predicates depend on, or whether a concurrent write actually lands before
// or after this transaction's guard runs — so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeleteUserCascade_Integration -tags integration -v

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

// createDeleteCascadeFixtureUser inserts a plain user row, then sets deletion_requested_at /
// manual_tier_override / manual_tier_ends_at directly via UPDATE, mirroring
// createClaimForDeletionFixtureUser's pattern. Returns the user re-read from the database
// (rather than the pre-write struct) so callers get the EXACT, DB-truncated
// deletion_requested_at value to use as "expected" — precisely what ExecuteDeletion's
// GetUserByID would have observed in production, avoiding any Go-vs-Postgres timestamp
// precision mismatch in the IS NOT DISTINCT FROM comparison under test.
func createDeleteCascadeFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	deletionRequestedAt *time.Time,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_delcasc_" + suffix,
		Email:              "delcasc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET deletion_requested_at = $1,
		    manual_tier_override = $2,
		    manual_tier_ends_at = $3
		WHERE id = $4`,
		deletionRequestedAt, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	fresh, err := repo.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	return fresh
}

// TestDeleteUserCascade_Integration_OrdinaryEligibleUser_Deletes is the control/regression case:
// an ordinary eligible user (no override, no outstanding deletion request — the retention-
// triggered path) with no linked items must still be deleted exactly as before this fix.
func TestDeleteUserCascade_Integration_OrdinaryEligibleUser_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "an ordinary eligible user must still be deleted")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.True(t, isDeleted)
}

// TestDeleteUserCascade_Integration_CompGrantedAfterClaim_NotDeleted proves C1's first race: an
// admin grants a comp AFTER ClaimForDeletion succeeded (simulated here directly against the row)
// but BEFORE DeleteUserCascade runs — the external-cleanup window. The user must NOT be deleted.
func TestDeleteUserCascade_Integration_CompGrantedAfterClaim_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	// Retention-triggered path: no outstanding deletion request at claim time.
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: the user must be claimable before the comp lands")

	// Simulates an admin granting a comp during the external-cleanup window (Paddle cancel,
	// Plaid disconnect) between ClaimForDeletion succeeding and DeleteUserCascade running.
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		tier, overrideEndsAt, user.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "a comp granted after the claim but before the cascade must abort the deletion")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted, "the user must NOT have been deleted")
}

// TestDeleteUserCascade_Integration_DeletionCancelledAfterClaim_NotDeleted is the highest-value
// test in this task (Issue #1413 round-4 C1): a user with their OWN outstanding GDPR deletion
// request cancels it during the external-cleanup window, after ClaimForDeletion already
// succeeded but before DeleteUserCascade runs. The user carries NO manual tier override at all,
// so reusing claimForDeletionPredicate alone would NOT catch this (that predicate treats "no
// override" as sufficient authorization regardless of deletion_requested_at) — only the
// additional exact-value re-check this fix adds closes it. The user must NOT be deleted.
func TestDeleteUserCascade_Integration_DeletionCancelledAfterClaim_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	// GDPR self-requested deletion path: outstanding request at claim time, NO override at all.
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, nil, nil)
	require.NotNil(t, user.DeletionRequestedAt, "fixture sanity check")

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: the user must be claimable before cancelling")

	// The user cancels their own deletion request during the external-cleanup window.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	// DeleteUserCascade is called with the STALE expected value ClaimForDeletion observed
	// (user.DeletionRequestedAt, captured before the cancellation) — exactly what ExecuteDeletion
	// would pass, since it never re-reads deletion_requested_at between the claim and the cascade.
	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "a deletion request cancelled after the claim but before the cascade must abort the deletion — this is the C1 data-loss scenario")

	var isDeleted bool
	var deletionRequestedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted, deletion_requested_at FROM users WHERE id = $1", user.ID).Scan(&isDeleted, &deletionRequestedAt))
	assert.False(t, isDeleted, "the user must NOT have been deleted")
	assert.Nil(t, deletionRequestedAt, "the cancellation must stick — the cascade must not have touched this row at all")
}

// TestDeleteUserCascade_Integration_OwnGDPRRequestStillOutstanding_DeletedDespiteComp proves the
// deliberate precedence must survive the new guard: a user's OWN outstanding GDPR deletion
// request beats an admin comp. An over-tight guard that blocks legitimate GDPR deletions whenever
// ANY override is present must fail this test.
func TestDeleteUserCascade_Integration_OwnGDPRRequestStillOutstanding_DeletedDespiteComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "a user's own outstanding GDPR deletion request must beat a live admin comp at claim time")

	// Nothing changes deletion_requested_at between the claim and the cascade in this scenario.
	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "a user's own outstanding GDPR deletion request must still beat a live comp inside the cascade guard too")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.True(t, isDeleted)
}

// TestDeleteUserCascade_Integration_UncleanedPlaidItemRemains_NotDeleted proves C1's companion
// finding C2: DeleteUserCascade must refuse to run while any linked_items row still needs
// Plaid-side cleanup (status <> 'disconnected') — deleting the local row would orphan a live,
// still-billing Plaid access token with no local record it ever existed.
func TestDeleteUserCascade_Integration_UncleanedPlaidItemRemains_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	// Default status from Create is 'active' — i.e. NOT yet cleaned up at Plaid.
	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID) })

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "the cascade must not run while an un-cleaned Plaid item remains")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted, "the user must NOT have been deleted")

	var itemCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemCount))
	assert.Equal(t, 1, itemCount, "the linked_items row — the only pointer to the live Plaid token — must survive the aborted cascade")
}

// TestDeleteUserCascade_Integration_AllItemsDisconnected_Deletes is the C2 control case: once
// every linked_items row is 'disconnected' (Plaid cleanup already ran), the cascade proceeds
// normally and the linked_items rows are actually removed.
func TestDeleteUserCascade_Integration_AllItemsDisconnected_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	_, err := pool.Exec(ctx, `UPDATE linked_items SET status = 'disconnected' WHERE id = $1`, item.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "the cascade must proceed once every linked item is already disconnected")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.True(t, isDeleted)

	var itemCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemCount))
	assert.Equal(t, 0, itemCount, "Step 3 of the cascade must have actually deleted the linked_items row")
}
