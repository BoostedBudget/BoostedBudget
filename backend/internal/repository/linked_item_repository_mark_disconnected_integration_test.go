//go:build integration

package repository

// Integration tests for LinkedItemRepository.MarkItemsDisconnected (Issue #1413 H2).
//
// PlaidItemCleanupService's RemoveAllUserItems/DisconnectAllUserItems used to track per-item
// success/failure only in memory, then (for DisconnectAllUserItems) blindly mark EVERY item for
// the user as 'disconnected' regardless of whether that specific item's own Plaid call actually
// succeeded. That masked genuine per-item failures: a failed item's row would still say
// 'disconnected' even though it was never removed at Plaid, so GetActiveItemsByUserID would
// never surface it again for a retry. MarkItemsDisconnected persists per-item outcome instead —
// pgxmock can only match query text, not whether `id = ANY($2)` actually left the OTHER item's
// row untouched, so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_MarkItemsDisconnected -tags integration -v

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

// createMarkDisconnectedFixtureUser inserts a bare user row for linked-item fixtures, registering
// cleanup. Mirrors profile_repository_claim_for_deletion_integration_test.go's fixture helper.
func createMarkDisconnectedFixtureUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, profileRepo ProfileRepository, suffix string) string {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_markdisc_" + suffix,
		Email:              "markdisc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })
	return user.ID
}

// createMarkDisconnectedFixtureItem inserts a single active linked_items row for userID.
func createMarkDisconnectedFixtureItem(t *testing.T, ctx context.Context, repo LinkedItemRepository, userID, suffix string) *domain.LinkedItem {
	t.Helper()

	item := &domain.LinkedItem{
		UserID:      userID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, repo.Create(ctx, item))
	return item
}

// TestLinkedItemRepository_MarkItemsDisconnected_OnlyMarksGivenItems is the falsifiability-
// critical test: an implementation that (like the old MarkAllUserItemsDisconnected call site)
// marks EVERY item for the user, rather than only the ids passed in, would fail the "item B
// untouched" assertions below.
func TestLinkedItemRepository_MarkItemsDisconnected_OnlyMarksGivenItems(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)

	itemA := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-a")
	itemB := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-b")

	err := repo.MarkItemsDisconnected(ctx, []string{itemA.ID})
	require.NoError(t, err)

	gotA, err := repo.GetByID(ctx, itemA.ID)
	require.NoError(t, err)
	assert.Equal(t, string(domain.LinkedItemStatusDisconnected), string(gotA.Status), "the given item must be marked disconnected")

	gotB, err := repo.GetByID(ctx, itemB.ID)
	require.NoError(t, err)
	assert.NotEqual(t, string(domain.LinkedItemStatusDisconnected), string(gotB.Status), "an item NOT in the given list must be left untouched")

	active, err := repo.GetActiveItemsByUserID(ctx, userID)
	require.NoError(t, err)
	ids := make([]string, 0, len(active))
	for _, it := range active {
		ids = append(ids, it.ID)
	}
	assert.NotContains(t, ids, itemA.ID, "a disconnected item must not be returned as active — this is what lets a retry skip it")
	assert.Contains(t, ids, itemB.ID, "the untouched item must still be returned as active — this is what lets a retry pick it up")
}

// TestLinkedItemRepository_MarkItemsDisconnected_EmptyIDsIsNoop proves an empty/nil ids slice
// does not error and does not touch any row (no ANY($2) call with an empty array that could
// otherwise be misinterpreted).
func TestLinkedItemRepository_MarkItemsDisconnected_EmptyIDsIsNoop(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	require.NoError(t, repo.MarkItemsDisconnected(ctx, nil))

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.NotEqual(t, string(domain.LinkedItemStatusDisconnected), string(got.Status), "a nil ids slice must not disconnect anything")
}

// TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted proves the WHERE
// status NOT IN ('disconnected', 'replaced') guard: a replaced item passed in by mistake must not
// be flipped back to 'disconnected' — 'replaced' is a terminal soft-delete state from a re-link,
// not an active connection that can be un-disconnected.
func TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	_, err := pool.Exec(ctx, `UPDATE linked_items SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, item.ID)
	require.NoError(t, err)

	require.NoError(t, repo.MarkItemsDisconnected(ctx, []string{item.ID}))

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, string(domain.LinkedItemStatusReplaced), string(got.Status), "a replaced item must not be reverted to disconnected")
}
