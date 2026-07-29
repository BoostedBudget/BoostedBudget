//go:build integration

package repository

// Integration tests for GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window
// repair path, Codex review).
//
// Issue #1413 round-10: the grace-window anchor is now plaid_cleanup_pending_at, NOT
// retention_started_at — rounds 8/9 demonstrated retention_started_at cannot be made sound for
// this purpose (written by several independent paths, its clearing can fail silently, and it is
// never cleared once a disconnect completes since it also drives the separate 6-month
// retention/deletion clock). Every test below that previously called setRetentionStartedAt to
// control the grace window now calls setPlaidCleanupPendingAt instead — see
// TestPlaidDisconnectRepair_Integration_PrematureDisconnect_StaleRetentionAnchor_FreshMarker_Excluded
// for the round-9 regression this replacement anchor exists to close: a STALE retention_started_at
// combined with a genuinely fresh plaid_cleanup_pending_at must NOT trigger a premature disconnect.
//
// TrialService.ExpireTrial no longer calls Plaid at all — no ordering of a status flip and an
// irreversible Plaid call can be safe, because /subscription/checkout is deliberately whitelisted
// for read-only users (internal/huma/middleware/read_only.go), so becoming read-only never stops a
// user from becoming active again. The fix defers instead of ordering: this query is the ONLY
// place a read-only user's Plaid items are ever disconnected, and it only selects a user once
// gracePeriod has elapsed since plaid_cleanup_pending_at was set (atomically alongside the status
// flip to read-only by ExpireTrialIfNoOverride/CancelIfPastDueWithRetention(Occurrence)/
// UpdateSubscriptionFromPaddle — see profile_repository.go) — giving a genuine resubscriber a
// window to become active again before Plaid is ever touched. pgxmock cannot validate the
// EXISTS-subquery/WHERE predicates or the grace-window arithmetic here — a real DB is required.
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
// that need to distinguish "within" from "past" the window set plaid_cleanup_pending_at (Issue
// #1413 round-10 grace anchor) relative to this value, not to production's constant directly — if
// production's grace period ever changes, these tests keep testing the query's BEHAVIOR (honor
// whatever period is passed) rather than silently asserting against a stale copy of the production
// number.
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

// setRetentionStartedAt back-dates (or sets) a user's retention_started_at directly via SQL. This
// was the round-7 grace anchor; round-10 replaced it with plaid_cleanup_pending_at (see
// setPlaidCleanupPendingAt below) because retention_started_at cannot be made sound for that
// purpose (see this file's header comment). This helper still exists for tests that need to model
// retention_started_at's OWN, unrelated purpose (the 6-month retention/deletion clock) — in
// particular TestPlaidDisconnectRepair_Integration_PrematureDisconnect_StaleRetentionAnchor_FreshMarker_Excluded,
// which deliberately sets a STALE retention_started_at alongside a FRESH plaid_cleanup_pending_at
// to prove the two are now fully independent.
func setRetentionStartedAt(t *testing.T, pool *pgxpool.Pool, userID string, startedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), "UPDATE users SET retention_started_at = $1 WHERE id = $2", startedAt, userID)
	require.NoError(t, err)
}

// setPlaidCleanupPendingAt sets a user's plaid_cleanup_pending_at marker directly via SQL — the
// Issue #1413 round-10 grace-window anchor GetUsersNeedingPlaidDisconnectRepair now uses
// EXCLUSIVELY (see this file's header comment). CreateUserTx never sets this column, so every
// fixture in this file that needs to control the grace window sets it explicitly.
func setPlaidCleanupPendingAt(t *testing.T, pool *pgxpool.Pool, userID string, pendingAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), "UPDATE users SET plaid_cleanup_pending_at = $1 WHERE id = $2", pendingAt, userID)
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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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
// Falsifiability: dropping the `plaid_cleanup_pending_at < cutoff` clause from the query entirely
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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-1*time.Hour))

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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

	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an outstanding GDPR deletion request must never be a repair candidate")
	}
}

// TestPlaidDisconnectRepairEligibleStatus_Integration_ParityWithGo is the Issue #1413 round-8
// High 4 parity test (Codex review): proves plaidDisconnectRepairEligibleStatusPredicate (the SQL
// half, profile_repository.go) and domain.SubscriptionStatus.IsEligibleForPlaidDisconnectRepair()
// (the Go half, used by retention_cleanup_check.go's per-item recheck) agree for every subscription
// status value. Mirrors this branch's existing precedent for this exact failure class —
// noActiveManualOverridePredicate / domain.User.HasActiveManualOverride(), guarded by
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo
// (profile_repository_manual_override_retention_integration_test.go) — one shared definition of
// "which statuses are eligible", kept honest by running the EXACT production SQL constant against
// the EXACT production Go method, not a hand-copied re-statement of either.
//
// Falsifiability: reverting the Go recheck (or this predicate) to include/exclude 'past_due'
// differently than the other makes this test fail.
func TestPlaidDisconnectRepairEligibleStatus_Integration_ParityWithGo(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	statuses := []domain.SubscriptionStatus{
		domain.SubscriptionStatusActive,
		domain.SubscriptionStatusInactive,
		domain.SubscriptionStatusTrial,
		domain.SubscriptionStatusTrialing,
		domain.SubscriptionStatusCancelled,
		domain.SubscriptionStatusPastDue,
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	type statusRow struct {
		status domain.SubscriptionStatus
		userID string
	}
	var rows []statusRow

	for i, status := range statuses {
		id := fmt.Sprintf("plaidstatusparity_%s_%d", suffix, i)
		user := &domain.User{
			ClerkUserID:        "clerk_" + id,
			Email:              id + "@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: string(status),
			PrimaryRegion:      "iad",
			IsActive:           true,
		}
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.CreateUserTx(ctx, tx, user))
		require.NoError(t, tx.Commit(ctx))
		userID := user.ID
		t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

		rows = append(rows, statusRow{status: status, userID: userID})
	}

	require.Len(t, rows, len(statuses), "one fixture per known subscription status")

	// Ask the SQL predicate, for every inserted row, which ids it considers eligible. This runs
	// the EXACT production constant, not a hand-copied re-statement of it.
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.userID
	}

	sqlRows, err := pool.Query(ctx, `SELECT id FROM users WHERE id = ANY($1) AND `+plaidDisconnectRepairEligibleStatusPredicate, ids)
	require.NoError(t, err)
	sqlEligible := make(map[string]bool, len(rows))
	for sqlRows.Next() {
		var id string
		require.NoError(t, sqlRows.Scan(&id))
		sqlEligible[id] = true
	}
	require.NoError(t, sqlRows.Err())
	sqlRows.Close()

	for _, r := range rows {
		goSaysEligible := r.status.IsEligibleForPlaidDisconnectRepair()
		sqlSaysEligible := sqlEligible[r.userID]
		if goSaysEligible != sqlSaysEligible {
			t.Errorf("parity mismatch for status=%q: Go.IsEligibleForPlaidDisconnectRepair()=%v, SQL plaidDisconnectRepairEligibleStatusPredicate says eligible=%v",
				r.status, goSaysEligible, sqlSaysEligible)
		}
	}
}

// ─── Issue #1413 round-10: plaid_cleanup_pending_at replaces retention_started_at as the sole
// grace-window anchor ──

// TestPlaidDisconnectRepair_Integration_NullMarker_Excluded proves the marker's core invariant:
// NULL means nothing pending, and the repair pass must never invent work from a derived signal.
// An inactive/cancelled user with a live linked item but NO plaid_cleanup_pending_at set (the
// default for every user — CreateUserTx never touches this column) must never be a repair
// candidate, no matter how long ago they were created.
//
// Falsifiability: replacing the `plaid_cleanup_pending_at IS NOT NULL AND ... < $1` clause with
// just `retention_started_at < $1` (the pre-round-10 anchor, which this fixture never sets either)
// would coincidentally still exclude this user here, but replacing it with a looser check that
// treats NULL as "always eligible" (e.g. dropping the IS NOT NULL clause and letting SQL's `NULL <
// $1` evaluate to UNKNOWN in a context that defaults to including it) would make this test fail.
func TestPlaidDisconnectRepair_Integration_NullMarker_Excluded(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_nullmarker_" + suffix,
		Email:              "nullmarker-" + suffix + "@example.com",
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
	// plaid_cleanup_pending_at deliberately left NULL (CreateUserTx's default) — nothing pending.

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID,
			"a user with NO plaid_cleanup_pending_at marker must never be a repair candidate — NULL means nothing pending")
	}
}

// TestPlaidDisconnectRepair_Integration_PrematureDisconnect_StaleRetentionAnchor_FreshMarker_Excluded
// is the round-9 High regression this whole redesign exists to close, and the test the task brief
// calls out by name: a user with a STALE retention_started_at (4 days ago — itself past the old
// anchor's 72h grace window, left over from an earlier cycle) combined with a GENUINELY RECENT
// cancellation (plaid_cleanup_pending_at set only 1 day ago, still well within the 72h grace
// window) must NOT be disconnected. Rounds 8/9 both failed to prevent this: retention_started_at
// is written by several independent paths and can go stale across a resubscribe-then-recancel
// cycle, and the round-8 "implausibly old" guard only caught staleness older than the ENTIRE
// 6-month retention period — a 4-day-old value sailed straight through it.
//
// Falsifiability: this test FAILS against the pre-round-10 query (`retention_started_at < $1`,
// which this fixture deliberately makes stale) — that is the whole point. It must pass against
// today's query, which anchors on plaid_cleanup_pending_at alone and never reads
// retention_started_at at all.
func TestPlaidDisconnectRepair_Integration_PrematureDisconnect_StaleRetentionAnchor_FreshMarker_Excluded(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_prematuredc_" + suffix,
		Email:              "prematuredc-" + suffix + "@example.com",
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
	// STALE decoy: a retention_started_at left over from an earlier cycle, 4 days ago — well past
	// the 72h grace window the OLD (pre-round-10) design would have anchored on.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-4*24*time.Hour))
	// FRESH truth: the genuine cancellation this repair pass must actually respect happened only
	// 1 day ago — nowhere near the 72h grace window.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-24*time.Hour))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID,
			"a stale multi-day retention_started_at must NOT trigger a disconnect for a genuinely recent (1-day-old) cancellation — "+
				"the repair pass must anchor exclusively on plaid_cleanup_pending_at")
	}
}

// TestPlaidDisconnectRepair_Integration_NeverStranded_FreshLookingRetentionAnchor_MarkerPastGrace_Included
// is the mirror-image positive control: retention_started_at looks RECENT (1 hour ago — the kind
// of value that would have kept a user safely WITHIN the old anchor's grace window), but
// plaid_cleanup_pending_at is genuinely past the grace window (100 hours ago). The user MUST still
// be selected — proving inclusion is driven by the marker alone, so a user is never permanently
// stranded (never disconnected) merely because retention_started_at happens to look fresh or is
// otherwise unreliable.
func TestPlaidDisconnectRepair_Integration_NeverStranded_FreshLookingRetentionAnchor_MarkerPastGrace_Included(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_neverstranded_" + suffix,
		Email:              "neverstranded-" + suffix + "@example.com",
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
	// retention_started_at looks fresh/recent — irrelevant to this query now.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-1*time.Hour))
	// plaid_cleanup_pending_at is genuinely past the 72h grace window.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found,
		"a user must be selected once plaid_cleanup_pending_at is past the grace window, regardless of what retention_started_at looks like — never permanently stranded")
}

// TestProfileRepository_ClearPlaidCleanupPending_Integration proves ClearPlaidCleanupPending
// actually nulls the column (called by repairPlaidDisconnects once a disconnect completes
// successfully — retention_cleanup_check.go) and that a subsequent candidate query no longer
// selects the user, even though they are still otherwise eligible (status, live item).
func TestProfileRepository_ClearPlaidCleanupPending_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearpending_" + suffix,
		Email:              "clearpending-" + suffix + "@example.com",
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
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))

	candidatesBefore, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	foundBefore := false
	for _, c := range candidatesBefore {
		if c.ID == user.ID {
			foundBefore = true
		}
	}
	require.True(t, foundBefore, "precondition: user must be a candidate before clearing the marker")

	require.NoError(t, repo.ClearPlaidCleanupPending(ctx, user.ID))

	var pendingAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&pendingAt))
	assert.Nil(t, pendingAt, "ClearPlaidCleanupPending must null the column")

	candidatesAfter, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidatesAfter {
		assert.NotEqual(t, user.ID, c.ID, "a cleared marker must remove the user from the candidate list even though status/live-item are unchanged")
	}
}
