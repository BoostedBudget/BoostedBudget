//go:build integration

package repository

// Integration tests for GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-6 repair path).
//
// TrialService.ExpireTrial's round-5 reordering (disconnect Plaid BEFORE the CAS flip) was itself
// a regression: 'trial' is a WRITABLE status, so a user could complete checkout or link a new
// account WHILE the disconnect ran. The round-6 fix restores CAS-FIRST ordering (flip, then
// disconnect), which eliminates that writable window instead of narrowing it — but it also means a
// disconnect that fails, or never runs at all (process killed between the flip and the disconnect
// call), leaves the user stuck: already read-only, but with live Plaid items
// GetUsersWithExpiredTrials can never re-select, since subscription_status is no longer 'trial'.
// This query is the backstop, mirroring GetUsersNeedingRetentionClockRepair's own integration test
// shape exactly. pgxmock cannot validate the EXISTS-subquery/WHERE predicates here — a real DB is
// required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestPlaidDisconnectRepair_Integration -tags integration -v

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

// seedLinkedItem inserts a minimal linked_items row for userID with the given status ("active",
// "disconnected", "replaced", ...). Mirrors the minimal-column INSERT pattern already used by
// other integration tests in this package (e.g. account_create_integration_test.go) — only the
// columns GetUsersNeedingPlaidDisconnectRepair's EXISTS subquery actually inspects matter here.
// status = "replaced" also stamps replaced_at, to satisfy the linked_items_replaced_at_consistency
// CHECK constraint (migration 000032).
func seedLinkedItem(t *testing.T, pool *pgxpool.Pool, userID, itemID, status string) {
	t.Helper()
	var replacedAt any
	if status == "replaced" {
		replacedAt = time.Now()
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status, replaced_at)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', $3, $4)
	`, userID, itemID, status, replacedAt)
	require.NoError(t, err)
}

func TestPlaidDisconnectRepair_Integration_FindsCancelledUserWithLiveItem(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidrepair_" + suffix,
		Email:              "plaidrepair-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "an inactive user with a live (active) linked item must be a repair candidate")
}

func TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoLiveItems(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_nolive_" + suffix,
		Email:              "nolive-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	// Both items already fully cleaned up — neither is "live".
	seedLinkedItem(t, pool, user.ID, "item-disc-"+suffix, "disconnected")
	seedLinkedItem(t, pool, user.ID, "item-repl-"+suffix, "replaced")

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a read-only-status user with no LIVE linked items must never be a repair candidate")
	}
}

func TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoItemsAtAll(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_noitems_" + suffix,
		Email:              "noitems-" + suffix + "@example.com",
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

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a read-only-status user who never linked any item must never be a repair candidate")
	}
}

func TestPlaidDisconnectRepair_Integration_ExcludesActiveUser(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidactive_" + suffix,
		Email:              "plaidactive-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	// A live item on a currently-active (non-read-only) user is completely normal — must never
	// be flagged for repair. This is the exact case the round-5 regression this repair backstops
	// would otherwise have destroyed: a subscribe-race user with genuinely live banking access.
	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "an active (non-read-only) user must never be a repair candidate, regardless of live items")
	}
}

func TestPlaidDisconnectRepair_Integration_ExcludesComped(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidcomp_" + suffix,
		Email:              "plaidcomp-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	// Lapsed override (ends in the past) — noManualOverrideAtAllPredicate excludes ANY override,
	// live or lapsed, mirroring the other retention/repair queries.
	past := time.Now().Add(-24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, past, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with ANY manual_tier_override (live or lapsed) must never be a repair candidate")
	}
}

func TestPlaidDisconnectRepair_Integration_ExcludesDeletionPending(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaiddelpending_" + suffix,
		Email:              "plaiddelpending-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an outstanding GDPR deletion request must never be a repair candidate")
	}
}
