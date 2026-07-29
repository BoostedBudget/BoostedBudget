//go:build integration

package repository

// Integration tests for GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window
// repair path, Codex review).
//
// TrialService.ExpireTrial no longer calls Plaid at all — no ordering of a status flip and an
// irreversible Plaid call can be safe, because /subscription/checkout is deliberately whitelisted
// for read-only users (internal/huma/middleware/read_only.go), so becoming read-only never stops a
// user from becoming active again. The fix defers instead of ordering: this query is the ONLY
// place a read-only user's Plaid items are ever disconnected, and it only selects a user once
// gracePeriod has elapsed since retention_started_at (set at the same moment the status flipped to
// read-only by ExpireTrial/CancelIfPastDueWithRetention/subscription-cancellation) — giving a
// genuine resubscriber a window to become active again before Plaid is ever touched. pgxmock
// cannot validate the EXISTS-subquery/WHERE predicates or the grace-window arithmetic here — a
// real DB is required.
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

// testGracePeriod is the grace duration used by these tests — deliberately the SAME shape as
// production's plaidDisconnectRepairGracePeriod (retention_cleanup_check.go), but redeclared here
// rather than imported (that constant lives in the jobs package, and importing it here would
// create an import cycle risk / unnecessary cross-package coupling for a test-only value). Tests
// that need to distinguish "within" from "past" the window set retention_started_at relative to
// this value, not to production's constant directly — if production's grace period ever changes,
// these tests keep testing the query's BEHAVIOR (honor whatever period is passed) rather than
// silently asserting against a stale copy of the production number.
const testGracePeriod = 72 * time.Hour

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

// seedLinkedItemNullStatus inserts a linked_items row with status = NULL — linked_items.status is
// nullable (migration 000013 added it with no NOT NULL constraint), so a row inserted without an
// explicit status (or predating the 'active' default) can genuinely have a NULL status while still
// being fully "live". Issue #1413 round-7 finding (Codex review): a bare
// `status NOT IN ('disconnected', 'replaced')` evaluates to SQL's UNKNOWN (not TRUE) for a NULL
// status, silently excluding this row — exactly the leak GetUsersNeedingPlaidDisconnectRepair
// exists to prevent. See TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive.
func seedLinkedItemNullStatus(t *testing.T, pool *pgxpool.Pool, userID, itemID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', NULL)
	`, userID, itemID)
	require.NoError(t, err)
}

// setRetentionStartedAt back-dates (or sets) a user's retention_started_at directly via SQL — the
// round-7 grace anchor. CreateUserTx never sets this column (it is only ever written by
// ExpireTrial/CancelIfPastDueWithRetention/RepairRetentionClock, all of which set it to "now" at
// the moment of the write), so every fixture in this file must set it explicitly to model a user
// who became read-only at a specific point in the past.
func setRetentionStartedAt(t *testing.T, pool *pgxpool.Pool, userID string, startedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), "UPDATE users SET retention_started_at = $1 WHERE id = $2", startedAt, userID)
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
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "an inactive user with a live (active) linked item, past the grace window, must be a repair candidate")
}

// TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive is the round-7 RED test (Codex
// review): linked_items.status is nullable, and a live item with a NULL status must still be
// selected for repair — the exact leak a bare `status NOT IN (...)` predicate causes via SQL's
// three-valued logic (NULL NOT IN (...) is UNKNOWN, not TRUE, so such a row is silently excluded).
//
// Falsifiability: reverting the predicate to `linked_items.status NOT IN ('disconnected',
// 'replaced')` (dropping the `IS NULL OR` clause) makes this test fail — the NULL-status item
// would never be found.
func TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidnullstatus_" + suffix,
		Email:              "plaidnullstatus-" + suffix + "@example.com",
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

	seedLinkedItemNullStatus(t, pool, user.ID, "item-nullstatus-"+suffix)
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "a live linked item with a NULL status must still be selected for repair — "+
		"NULL is not 'disconnected' or 'replaced', so it counts as live")
}

// TestPlaidDisconnectRepair_Integration_WithinGraceWindow_Excluded is the brief's other RED case:
// a read-only user with a live item, but WITHIN the grace window, must NOT be selected — this is
// what turns "resubscribe before disconnect" into a non-event rather than a narrowed race.
//
// Falsifiability: dropping the `retention_started_at < cutoff` clause from the query entirely
// makes this test fail — the user would be selected immediately, with no grace window at all.
func TestPlaidDisconnectRepair_Integration_WithinGraceWindow_Excluded(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_withingrace_" + suffix,
		Email:              "withingrace-" + suffix + "@example.com",
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
	// Became read-only just 1 hour ago — well within the 72h grace window.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-1*time.Hour))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID,
			"a read-only user WITHIN the grace window must NEVER be selected for Plaid disconnect")
	}
}

// TestPlaidDisconnectRepair_Integration_PastGraceWindow_Included is
// WithinGraceWindow_Excluded's positive-control sibling: the SAME user shape, but read-only for
// longer than the grace period, MUST be selected — proving the exclusion above is the grace
// window doing its job, not some unrelated bug that always excludes everyone.
func TestPlaidDisconnectRepair_Integration_PastGraceWindow_Included(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_pastgrace_" + suffix,
		Email:              "pastgrace-" + suffix + "@example.com",
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
	// Became read-only 100 hours ago — past the 72h grace window.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "a read-only user PAST the grace window with a live item must be selected for repair")
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
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
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

	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
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
	// be flagged for repair. This is the exact case the round-7 design would otherwise destroy if
	// it selected on live-item-existence alone: a genuinely active user with live banking access.
	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")
	// Even past the grace window, an active user must never be selected — the status filter, not
	// the grace window, is what protects them here.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
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
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	// Lapsed override (ends in the past) — noManualOverrideAtAllPredicate excludes ANY override,
	// live or lapsed, mirroring the other retention/repair queries.
	past := time.Now().Add(-24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, past, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
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
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an outstanding GDPR deletion request must never be a repair candidate")
	}
}
