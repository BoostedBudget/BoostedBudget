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
// flip to read-only by ExpireTrialIfNoOverride/CancelIfPastDueOccurrenceWithRetention/
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/pgtime"
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

// seedLinkedItemNullStatus inserts a linked_items row with status = NULL — linked_items.status was
// nullable (migration 000013 added it with no NOT NULL constraint) until migration 000126 (Issue
// #1444) added that constraint, so a row inserted without an explicit status (or predating the
// 'active' default) could genuinely have a NULL status while still being fully "live". Issue #1413
// round-7 finding (Codex review): a bare `status NOT IN ('disconnected', 'replaced')` evaluates to
// SQL's UNKNOWN (not TRUE) for a NULL status, silently excluding this row — exactly the leak
// GetUsersNeedingPlaidDisconnectRepair exists to prevent. See
// TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive.
//
// The constraint arrives in 000126 rather than 000124 because #1444 adds it as a three-step
// transition — 000124 `ADD CONSTRAINT ... CHECK (status IS NOT NULL) NOT VALID`, 000125
// `VALIDATE CONSTRAINT`, 000126 `SET NOT NULL` + drop the scaffolding CHECK — so that the
// full-table validation scan runs under SHARE UPDATE EXCLUSIVE instead of blocking all reads and
// writes under ACCESS EXCLUSIVE. See .claude/skills/db-migration-guide/SKILL.md and
// internal/database/migration_000123_000126_integration_test.go.
//
// Takes the DB interface (not *pgxpool.Pool) so callers can pass a pgx.Tx: since migration 000126,
// a genuinely NULL status can no longer be constructed against the real schema at all — the only
// way to exercise this scenario is to transactionally DROP NOT NULL first (see
// TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive), which requires every statement in
// the test, including this seed, to run against that same uncommitted transaction.
func seedLinkedItemNullStatus(t *testing.T, db DB, userID, itemID string) {
	t.Helper()
	_, err := db.Exec(context.Background(), `
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
//
// Takes the DB interface (not *pgxpool.Pool) so it can also be used inside a transaction — see
// seedLinkedItemNullStatus's doc comment for why that matters post-#1444.
func setPlaidCleanupPendingAt(t *testing.T, db DB, userID string, pendingAt time.Time) {
	t.Helper()
	_, err := db.Exec(context.Background(), "UPDATE users SET plaid_cleanup_pending_at = $1 WHERE id = $2", pendingAt, userID)
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
//
// Since migration 000126 (Issue #1444), linked_items.status is NOT NULL — a genuinely NULL status
// can no longer be constructed against the real schema at all, in any migrated database. This test
// therefore runs its own transaction that transactionally DROPs the NOT NULL constraint first
// (rolled back, never committed, at the very end — the real schema is never permanently altered)
// so the scenario this test exists to guard can still be exercised. The application-level rule this
// asserts (a NULL status counts as live) remains correct defensive code even though the column can
// no longer produce one organically — see liveLinkedItemExistsClause's own doc comment.
func TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	repo := NewProfileRepository(tx, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidnullstatus_" + suffix,
		Email:              "plaidnullstatus-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))

	seedLinkedItemNullStatus(t, tx, user.ID, "item-nullstatus-"+suffix)
	setPlaidCleanupPendingAt(t, tx, user.ID, time.Now().Add(-testGracePeriod-time.Hour)) // past grace

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

// TestPlaidDisconnectRepair_Integration_ExcludesLiveComp is the Fix 2 positive control: a
// CURRENTLY-active override must still exclude a user from Plaid-disconnect repair —
// noActiveManualOverridePredicate (Issue #1413 round-12 Fix 2) keeps this half of the old
// noManualOverrideAtAllPredicate behavior; only the LAPSED half changes (see
// TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LapsedNeverRemoved_StillRepaired
// below).
func TestPlaidDisconnectRepair_Integration_ExcludesLiveComp(t *testing.T) {
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

	// LIVE override (ends in the future) — noActiveManualOverridePredicate must still exclude this.
	future := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with a CURRENTLY-ACTIVE manual_tier_override must never be a repair candidate")
	}
}

// TestPlaidDisconnectRepair_Integration_ExcludesIndefiniteComp closes a gap in the Plaid-repair
// suite (Issue #1413/#1443 gap analysis, PR #1443 AC9): every other "live comp" fixture in this
// file (TestPlaidDisconnectRepair_Integration_ExcludesLiveComp and its siblings below) sets
// manual_tier_ends_at to a finite FUTURE timestamp, which only exercises
// noActiveManualOverridePredicate's `manual_tier_ends_at >= NOW()` disjunct. Both
// domain.User.hasActiveManualOverrideAt and this predicate treat a NULL manual_tier_ends_at as a
// PERMANENT comp (never expires) via a SEPARATE `manual_tier_ends_at IS NULL` disjunct — an admin
// granting an override with no end date at all — and nothing in this suite had ever driven a row
// through that specific disjunct. Falsifiability: replacing
// `manual_tier_ends_at IS NULL OR manual_tier_ends_at >= NOW()` with
// `COALESCE(manual_tier_ends_at, '-infinity'::timestamptz) >= NOW()` in
// noActiveManualOverridePredicate (profile_repository.go) — a realistic bug that treats a NULL end
// date as already-expired — makes this test fail: GetUsersNeedingPlaidDisconnectRepair would then
// wrongly select this indefinitely-comped user for a live Plaid disconnect. (Simply deleting the
// `IS NULL OR` disjunct does NOT falsify this test: `manual_tier_ends_at >= NOW()` against a NULL
// column evaluates to SQL NULL/UNKNOWN, which the enclosing `NOT (... AND ...)` — and then the
// outer `WHERE ... AND` — both still treat as "exclude this row," so that particular mutation
// coincidentally preserves the correct behavior. The COALESCE mutation above is the one that
// actually changes the truth value and was used to verify this test red/green.)
func TestPlaidDisconnectRepair_Integration_ExcludesIndefiniteComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaidindefcomp_" + suffix,
		Email:              "plaidindefcomp-" + suffix + "@example.com",
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

	// Indefinite (permanent) comp: an override with NO end date at all — manual_tier_ends_at
	// stays NULL. noActiveManualOverridePredicate's `manual_tier_ends_at IS NULL` disjunct must
	// still exclude this user, distinct from the finite-future-end-date case above.
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = NULL WHERE id = $1`, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with an INDEFINITE (NULL end date) manual_tier_override must never be a repair candidate")
	}
}

// TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LapsedNeverRemoved_StillRepaired
// is the Issue #1413 round-13 P0 regression test, built through PRODUCTION WRITERS ONLY — no raw
// `UPDATE users SET manual_tier_override ...` or `SET plaid_cleanup_pending_at ...` anywhere in this
// test's setup.
//
// Scenario: a user's Paddle subscription genuinely cancels (repo.UpdateSubscriptionFromPaddle sets
// plaid_cleanup_pending_at, the normal path — this is a real, already-churned account). An admin
// THEN comps this already-cancelled account (adminRepo.UpdateUserTier) — a plausible real action
// (e.g. a support gesture). expiresAt is already in the past, modeling a comp that has lapsed by
// the time anyone checks. RemoveUserTierOverride/RemoveUserTierOverrideIfMatches is deliberately
// NEVER called — this models the daily comp-expiry job degrading or simply never running.
//
// Per the restated invariant (domain.User.PlaidCleanupPendingAt's doc comment), granting a comp no
// longer nulls the marker: the marker records that an ineligibility EPISODE began and is durable
// across the comp's entire lifetime, live or lapsed. CURRENT eligibility (a live override) is
// evaluated separately, at query time, by GetUsersNeedingPlaidDisconnectRepair's own
// noActiveManualOverridePredicate. With the comp lapsed and never removed, that predicate no longer
// excludes this user, so the repair pass must find them — closing the leak where a comped-then
// -lapsed-then-never-expired user's bank connections stayed live forever, invisible to every query
// in the codebase.
//
// This replaces TestPlaidDisconnectRepair_Integration_LapsedCompStillRepaired, which passed only
// because its setup used raw `pool.Exec` SQL to write manual_tier_override/manual_tier_ends_at (and
// setPlaidCleanupPendingAt) directly. Under the OLD updateUserTierQuery — which unconditionally
// nulled plaid_cleanup_pending_at on every grant — that combination (marker present AND an override
// ever having been granted) was UNREACHABLE via any production writer: granting a comp always erased
// the marker, so the old test's hand-built row state could never actually occur. This version
// reaches the identical row state using only repo.UpdateSubscriptionFromPaddle and
// adminRepo.UpdateUserTier.
//
// Falsifiability: reverting updateUserTierQuery to unconditionally NULL plaid_cleanup_pending_at on
// grant makes this test fail — UpdateUserTier's own write would erase the marker this test relies on,
// and the user would never be selected.
func TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LapsedNeverRemoved_StillRepaired(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_complapsednevrm_" + suffix,
		Email:              "complapsednevrm-" + suffix + "@example.com",
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

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	// Real churn, via the real webhook-facing writer: the marker starts here (no override exists
	// yet, so nothing suppresses it).
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	// An admin comps this ALREADY-cancelled account. expiresAt is already in the past: the comp is
	// modeled as having lapsed by the time anyone checks, without needing to sleep in real
	// wall-clock time to simulate the passage of days.
	lapsedExpiry := time.Now().Add(-1 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: comp an already-churned account", &lapsedExpiry)
	require.NoError(t, err)

	// Deliberately NOT calling RemoveUserTierOverride / RemoveUserTierOverrideIfMatches — this
	// models the comp-expiry job degrading or never running at all.

	// The marker was stamped moments ago by UpdateSubscriptionFromPaddle's own Go time.Now(), in
	// THIS process; the query's cutoff below is computed the same way (time.Now().Add(-gracePeriod))
	// in this same process — this is a Go-time-to-Go-time comparison, not a Go-time-to-Postgres-NOW()
	// comparison, so no cross-clock fencing is needed here. A short sleep plus a near-zero grace
	// period is enough to deterministically put the marker "past grace" without any raw SQL
	// backdating.
	time.Sleep(5 * time.Millisecond)
	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "a comp granted over an already-cancelled user, then lapsed and never removed, must still be picked up by the repair pass — the marker must survive the grant")
}

// TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LiveComp_ExcludedFromRepair
// is the P0 fix's positive control, built the identical way (production writers only): the SAME
// "comp an already-cancelled account" scenario as the regression test above, but with the comp
// still LIVE (manual_tier_ends_at in the future). The marker survives the grant exactly as above,
// but the repair pass must still exclude this user — GetUsersNeedingPlaidDisconnectRepair's
// noActiveManualOverridePredicate keeps a currently-comped (paying-by-comp) user's bank connection
// untouched. Over-correcting the P0 leak into disconnecting a live comp would be worse than the
// leak itself.
//
// Falsifiability: dropping noActiveManualOverridePredicate from GetUsersNeedingPlaidDisconnectRepair
// makes this test fail — the user would be selected despite the live comp.
func TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LiveComp_ExcludedFromRepair(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_complivenevrm_" + suffix,
		Email:              "complivenevrm-" + suffix + "@example.com",
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

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	liveExpiry := time.Now().Add(30 * 24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: comp an already-churned account, still live", &liveExpiry)
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)
	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with a CURRENTLY-LIVE comp must never be selected for Plaid-disconnect repair, even though the marker (correctly) survived the grant")
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

// ─── Issue #1413 round 16: StampMissingPlaidCleanupMarkers, the self-healing backstop ──
//
// Five prior review rounds each closed one way plaid_cleanup_pending_at could end up NULL while a
// user still has a live linked item, and each fix opened another — because every attempt tried to
// make some WRITER maintain the marker correctly, and writers cannot: they race with the
// linked_items lifecycle they are trying to summarise (see removeUserTierOverrideSelectPrior's doc
// comment, admin_user_repository.go, and domain.User.PlaidCleanupPendingAt's doc comment, for the
// two concrete races — a re-fetch that just moves its own window, and a CTE snapshot that cannot
// serialize against an independent autocommit INSERT).
//
// StampMissingPlaidCleanupMarkers stops trying to fix writers and instead heals: run once at the
// start of every repairPlaidDisconnects pass (retention_cleanup_check.go), it atomically stamps
// plaid_cleanup_pending_at for any user who is ineligible-status, has at least one live linked item,
// and has a NULL marker. A missed or prematurely-cleared marker then self-heals on the very next
// pass instead of leaking the user's bank connection forever.
//
// TestStampMissingPlaidCleanupMarkers_Integration_HealsNullMarkerWithLiveItem is the headline RED
// test the task brief calls for: a user who is ineligible, has a live linked item, and a NULL
// marker must be selected for repair AFTER the backstop runs, and must NOT be selected BEFORE it.
// Reported failing (build failure — the method did not exist) before StampMissingPlaidCleanupMarkers
// was implemented; passing after.
func TestStampMissingPlaidCleanupMarkers_Integration_HealsNullMarkerWithLiveItem(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_stampheal_" + suffix,
		Email:              "stampheal-" + suffix + "@example.com",
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
	// plaid_cleanup_pending_at deliberately left NULL (CreateUserTx's default) — this is the exact
	// leaked state every prior round's writer-side fix could, under some race, still produce.

	// BEFORE: a NULL marker must never invent work from a derived signal — the user is not selected,
	// no matter how tight the grace period.
	before, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
	require.NoError(t, err)
	for _, c := range before {
		assert.NotEqual(t, user.ID, c.ID, "precondition: a NULL marker must exclude the user before the backstop runs")
	}
	var markerBefore *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&markerBefore))
	require.Nil(t, markerBefore, "precondition: the marker must genuinely be NULL before the backstop runs")

	// Issue #1413 Finding 3 (Codex review, PR #1443 follow-up, Medium): fence the Stamp call itself
	// with Postgres's OWN clock (dbNow, admin_user_repository_remove_tier_override_integration_test.go
	// — same package) rather than asserting only `markerAfter != nil`. A non-nil check alone couples
	// this test to nothing about the VALUE StampMissingPlaidCleanupMarkers actually writes — a broken
	// implementation that stamped an arbitrary, long-past timestamp (e.g. hardcoding some fixed date
	// instead of NOW()) would still satisfy "not nil" and this test would still go green. Bracketing
	// the call with dbNow before and after, both from the SAME clock the SQL itself uses (`NOW()`),
	// proves the stamped value is genuinely RECENT — produced by THIS call, not resurrected or
	// hardcoded — with no tolerance window needed (NOW() only advances, so before <= stamped <= after
	// holds exactly).
	fenceBefore := dbNow(t, pool)
	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	fenceAfter := dbNow(t, pool)

	var markerAfter *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&markerAfter))
	require.NotNil(t, markerAfter, "the backstop must stamp a marker for an ineligible user with a live item and a NULL marker")
	assertBetweenFence(t, fenceBefore, fenceAfter, *markerAfter,
		"the stamped marker must be Stamp's OWN fresh value (bracketed by a DB-clock fence around the call), not merely non-NULL — a broken Stamp that wrote an arbitrary stale timestamp must fail this assertion even though it satisfies require.NotNil above")

	// AFTER: past the grace period, the now-healed user must be selected.
	//
	// The two facts are asserted SEPARATELY and deliberately, because coupling them makes this test
	// flaky in CI IF the two sides of the comparison come from different clocks. They no longer do —
	// GetUsersNeedingPlaidDisconnectRepair now computes its cutoff entirely in Postgres
	// (`NOW() - make_interval(secs => $1)`, profile_repository.go), the SAME clock
	// plaid_cleanup_pending_at is stamped from, closing the cross-clock class that used to make this
	// split necessary (this method previously computed its cutoff as a GO-side
	// `time.Now().Add(-grace)`, which is precisely what made a Go-side sleep against a near-zero
	// grace period flaky here and in the migration-000121 tests). The split itself is kept anyway,
	// and is still the right shape: the fence assertion above proves WHAT Stamp wrote (its own fresh
	// value); this assertion proves a DIFFERENT fact — that the candidate query's selection logic
	// honors a grace boundary at all — and coupling them would still require sleeping in Go past an
	// arbitrary grace period to prove selection, which is both slower and needless now that a
	// Postgres-side back-date can simulate "past grace" deterministically. So: back-date the
	// (already fence-proven-fresh) marker by an hour using the package's established Postgres-side
	// helper, then query with a grace comfortably smaller than that backdate. Neither assertion
	// depends on any clock race, Go or Postgres, and both still test exactly what their names claim.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
	after, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Minute)
	require.NoError(t, err)
	found := false
	for _, c := range after {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "the healed user must be selected for repair once past the grace window — GetUsersNeedingPlaidDisconnectRepair itself is completely unchanged by this backstop")
}

// TestStampMissingPlaidCleanupMarkers_Integration_ExcludesUserWithNoLiveItems is the property that
// matters most about this backstop's shape: because it reuses liveLinkedItemExistsClause — the
// SAME constant GetUsersNeedingPlaidDisconnectRepair uses — a user with no live items is NEVER
// stamped, exactly preserving TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoLiveItems
// above without needing that pinned test to be reinterpreted in any way.
func TestStampMissingPlaidCleanupMarkers_Integration_ExcludesUserWithNoLiveItems(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_stampnolive_" + suffix,
		Email:              "stampnolive-" + suffix + "@example.com",
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

	// Both items already fully cleaned up — neither is "live" — mirrors
	// TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoLiveItems's fixture shape exactly.
	seedLinkedItem(t, pool, user.ID, "item-disc-"+suffix, "disconnected")
	seedLinkedItem(t, pool, user.ID, "item-repl-"+suffix, "replaced")

	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "a user with no LIVE linked items must never be stamped by the backstop, mirroring the candidate query's own live-item requirement")
}

// TestStampMissingPlaidCleanupMarkers_Integration_ExcludesCurrentlyComped proves the backstop
// mirrors GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate: it only ever
// creates a marker for a population the candidate query would itself eventually act on, never for a
// currently-comped user the candidate query would never select regardless of marker state.
func TestStampMissingPlaidCleanupMarkers_Integration_ExcludesCurrentlyComped(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_stampcomped_" + suffix,
		Email:              "stampcomped-" + suffix + "@example.com",
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
	future := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "a user with a CURRENTLY-ACTIVE manual_tier_override must not be stamped by the backstop — the candidate query would never select them anyway")
}

// TestStampMissingPlaidCleanupMarkers_Integration_Idempotent proves the backstop is safe to run on
// every single repair pass (its actual production cadence): once a marker is stamped, a second call
// must not disturb it — the WHERE clause's own `plaid_cleanup_pending_at IS NULL` makes every
// subsequent call for this user a no-op.
func TestStampMissingPlaidCleanupMarkers_Integration_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_stampidem_" + suffix,
		Email:              "stampidem-" + suffix + "@example.com",
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

	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	var firstStamp *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&firstStamp))
	require.NotNil(t, firstStamp)

	time.Sleep(5 * time.Millisecond)
	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	var secondStamp *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&secondStamp))
	require.NotNil(t, secondStamp)
	assert.True(t, firstStamp.Equal(*secondStamp), "a second backstop run must not re-stamp (and thus re-arm the grace window for) a marker that is already set")
}

// ─── Issue #1413 Finding 1 (Codex review, PR #1443 follow-up, headline finding):
// ClearCompletedPlaidCleanupMarkers, the self-healing backstop's symmetric CLEARING half ──────
//
// Until this fix, a marker left set after its episode's cleanup completed (e.g. every
// clearPlaidCleanupMarkerWithRetry attempt failing after a successful disconnect) was believed
// harmless — several comments across this branch called it "inert" or "untidy residue" the
// candidate query's own live-item EXISTS clause would filter out forever. That belief was WRONG: a
// comp granted and later removed over that stale marker (removeUserTierOverrideUpdate's COALESCE,
// admin_user_repository.go) preserves it instead of starting a fresh episode, so a bank the user
// relinks in the meantime inherits an already-expired grace window and gets disconnected
// immediately, with ZERO grace, the next repairPlaidDisconnects pass. See
// TestPlaidDisconnectRepair_Integration_StaleMarkerHealedBeforeRelink_FreshGraceWindowEnforced below
// for the full five-step scenario reproduced end to end.

// TestClearCompletedPlaidCleanupMarkers_Integration_ClearsStaleMarkerWithCompletedCleanup is the
// headline RED test the task brief calls for at the repository level: a user who is ineligible,
// whose marker is set, and whose ONLY linked item is already disconnected (cleanup for this episode
// genuinely completed) must have that marker cleared by ClearCompletedPlaidCleanupMarkers.
//
// Falsifiability: reverting ClearCompletedPlaidCleanupMarkers to a no-op (or deleting it) makes this
// test fail to compile — mirrors TestStampMissingPlaidCleanupMarkers_Integration_HealsNullMarkerWithLiveItem's
// own precedent for the Stamp half (reported failing via build error before that method existed).
func TestClearCompletedPlaidCleanupMarkers_Integration_ClearsStaleMarkerWithCompletedCleanup(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearcompleted_" + suffix,
		Email:              "clearcompleted-" + suffix + "@example.com",
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

	// Models the state left behind by a disconnect that succeeded but whose marker-clear retries all
	// failed: the ONLY linked item is already 'disconnected' (zero live), but the marker is still set,
	// stale — older than the grace period.
	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "disconnected")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))

	clearedCount, err := repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, clearedCount, 1, "at least this fixture's row must be cleared")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "a marker on a user whose cleanup has already completed (zero live items, at least one linked_items row) must be cleared")
}

// TestClearCompletedPlaidCleanupMarkers_Integration_KeepsMarkerWithZeroLinkedItemsAtAll pins the
// #1453 allowance, unchanged and mandatory: a user with ZERO linked_items rows at all (an ordinary
// never-linked-Plaid cancellation, or genuine 000119/000120 migration residue — indistinguishable at
// the row level, and both legitimate) must KEEP their marker. Clearing those would silently change
// behavior for a population this branch has deliberately, repeatedly left alone.
//
// Falsifiability: dropping the anyLinkedItemExistsClause condition (clearing on
// "NOT liveLinkedItemExistsClause" alone) makes this test fail — the marker would be nulled even
// though this user has never linked a single Plaid item.
func TestClearCompletedPlaidCleanupMarkers_Integration_KeepsMarkerWithZeroLinkedItemsAtAll(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearzerorows_" + suffix,
		Email:              "clearzerorows-" + suffix + "@example.com",
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

	// Deliberately NO seedLinkedItem call — this user has never linked Plaid at all (the #1453 shape).
	// Retain sub-microsecond input precision on macOS too; PostgreSQL cannot retain these nanos.
	staleMarker := time.Now().Add(-testGracePeriod - time.Hour).Truncate(time.Microsecond).Add(527 * time.Nanosecond)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker, "the #1453 allowance: a user with ZERO linked_items rows at all must KEEP their marker")
	assert.Equal(t, pgtime.Stored(staleMarker), marker.UTC(), "the marker's VALUE must be completely untouched, not just non-NULL")
}

// TestClearCompletedPlaidCleanupMarkers_Integration_ExcludesUserWithLiveItem proves the clear never
// touches a user who genuinely still has outstanding cleanup — mirrors
// TestStampMissingPlaidCleanupMarkers_Integration_ExcludesUserWithNoLiveItems's own shape for the
// opposite direction.
func TestClearCompletedPlaidCleanupMarkers_Integration_ExcludesUserWithLiveItem(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearlive_" + suffix,
		Email:              "clearlive-" + suffix + "@example.com",
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
	staleMarker := time.Now().Add(-testGracePeriod - time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker, "a user with at least one LIVE linked item must never have their marker cleared — cleanup is still genuinely outstanding")
	assert.Equal(t, pgtime.Stored(staleMarker), marker.UTC())
}

// TestClearCompletedPlaidCleanupMarkers_Integration_ClearsRegardlessOfActiveOverride proves the
// deliberate asymmetry with StampMissingPlaidCleanupMarkers (which DOES exclude a currently-comped
// user via noActiveManualOverridePredicate): Clear must NOT exclude one. If it did, a comp granted in
// the narrow window between a failed clear and this backstop's next scheduled pass would suppress
// the heal for as long as the comp stays active — reproducing Finding 1's exact bug on the very next
// removal, since removeUserTierOverrideUpdate's COALESCE only starts a fresh episode when the column
// is genuinely NULL.
func TestClearCompletedPlaidCleanupMarkers_Integration_ClearsRegardlessOfActiveOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearcomped_" + suffix,
		Email:              "clearcomped-" + suffix + "@example.com",
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

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "disconnected")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))
	future := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "unlike Stamp, Clear must NOT exclude a currently-comped user — the stale marker must be healed before, not after, a later comp removal's COALESCE can preserve it")
}

// TestClearCompletedPlaidCleanupMarkers_Integration_Idempotent proves the clear backstop is safe to
// run on every single pass: once a marker is cleared, a second call must be a pure no-op (nothing
// left to clear).
func TestClearCompletedPlaidCleanupMarkers_Integration_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_clearidem_" + suffix,
		Email:              "clearidem-" + suffix + "@example.com",
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

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "disconnected")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))

	firstClearedCount, err := repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, firstClearedCount, 1)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.Nil(t, marker)

	// A second run must not error and must not touch this already-NULL row again (there is nothing
	// left matching plaid_cleanup_pending_at IS NOT NULL for this user).
	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker)
}

// ─── Issue #1455: ClearMarkersOnEligibleStatus — the third backstop, closing invariant BRANCH 1 ──
//
// domain.User.PlaidCleanupPendingAt's invariant has two branches: (1) status NOT ineligible ⇒
// marker must be NULL; (2) status ineligible ⇒ marker non-NULL exactly while an episode is open.
// StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers above both self-heal branch
// 2, but both are gated on plaidDisconnectRepairEligibleStatusPredicate — neither ever looks at a
// row whose status is NOT inactive/cancelled. So until this fix, branch 1 had no runtime backstop:
// a row that ever ended up with a non-ineligible status AND a non-NULL marker stayed that way
// forever, and a later cancellation's COALESCE-free write could still inherit a marker stale enough
// to skip the 72-hour grace window entirely (see ClearMarkersOnEligibleStatus's own doc comment,
// profile_repository.go, for the full scenario).

func createClearMarkersOnEligibleStatusTestUser(t *testing.T, pool *pgxpool.Pool, repo *profileRepository, namePrefix, status string) *domain.User {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + namePrefix + "_" + suffix,
		Email:              namePrefix + "-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
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
	return user
}

// TestClearMarkersOnEligibleStatus_Integration_ClearsStaleMarkerOnNonIneligibleStatus is the RED
// test for Issue #1455: models the exact corruption the ticket describes — a row whose
// subscription_status is NOT inactive/cancelled but whose plaid_cleanup_pending_at is non-NULL
// (e.g. a hypothetical future status-only writer that forgets to touch this column, or historical
// corruption). No production writer can construct this today (PR #1443 deleted the only one that
// could — see this ticket's own "Current state" section), so this is modeled directly via raw SQL,
// matching this file's established convention (setPlaidCleanupPendingAt) for states no writer
// currently produces.
//
// Falsifiability: without ClearMarkersOnEligibleStatus (or if its WHERE clause were inverted), the
// marker stays non-NULL and this test fails.
func TestClearMarkersOnEligibleStatus_Integration_ClearsStaleMarkerOnNonIneligibleStatus(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createClearMarkersOnEligibleStatusTestUser(t, pool, repo, "branch1active", "active")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))

	clearedCount, err := repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, clearedCount, 1, "at least this fixture's row must be cleared")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "a marker on a user whose status is NOT ineligible must be cleared — branch 1 of the invariant")
}

// TestClearMarkersOnEligibleStatus_Integration_ExcludesIneligibleStatus proves this backstop never
// touches a row whose status genuinely IS inactive/cancelled — that population is
// StampMissingPlaidCleanupMarkers/ClearCompletedPlaidCleanupMarkers's job, not this one's. Mirrors
// TestClearCompletedPlaidCleanupMarkers_Integration_ExcludesUserWithLiveItem's shape for the
// opposite backstop.
func TestClearMarkersOnEligibleStatus_Integration_ExcludesIneligibleStatus(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createClearMarkersOnEligibleStatusTestUser(t, pool, repo, "branch1cancelled", "cancelled")
	staleMarker := time.Now().Add(-time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	_, err := repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker, "an ineligible-status (cancelled) user's marker must be untouched by this backstop")
	assert.Equal(t, pgtime.Stored(staleMarker), marker.UTC())
}

// TestClearMarkersOnEligibleStatus_Integration_ClearsRegardlessOfActiveOverride pins the Issue
// #1455 judgement call: unlike StampMissingPlaidCleanupMarkers (which excludes a currently-comped
// user because stamping only ever arms a marker for a population the repair candidate query would
// itself eventually act on, and that query already excludes a live comp), this backstop must NOT
// exclude one. domain.User.PlaidCleanupPendingAt's own doc comment is explicit that
// manual_tier_override is not a term in this invariant at all — branch 1 says "status not
// ineligible ⇒ marker must be NULL", full stop, independent of override state. Mirrors
// ClearCompletedPlaidCleanupMarkers's identical asymmetry (which also does not exclude an active
// override) rather than Stamp's.
func TestClearMarkersOnEligibleStatus_Integration_ClearsRegardlessOfActiveOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createClearMarkersOnEligibleStatusTestUser(t, pool, repo, "branch1comped", "active")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
	future := time.Now().Add(24 * time.Hour)
	_, err := pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, future, user.ID)
	require.NoError(t, err)

	clearedCount, err := repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, clearedCount, 1)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker, "unlike Stamp, this backstop must NOT exclude a currently-comped user — branch 1 doesn't reference manual_tier_override at all")
}

// TestClearMarkersOnEligibleStatus_Integration_ExcludesSoftDeletedUser proves the is_deleted = FALSE
// guard is real, matching this file's convention of testing every clause in a global-scan WHERE.
func TestClearMarkersOnEligibleStatus_Integration_ExcludesSoftDeletedUser(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createClearMarkersOnEligibleStatusTestUser(t, pool, repo, "branch1deleted", "active")
	staleMarker := time.Now().Add(-time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)
	_, err := pool.Exec(ctx, `UPDATE users SET is_deleted = TRUE WHERE id = $1`, user.ID)
	require.NoError(t, err)

	_, err = repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker, "a soft-deleted user must be untouched by this backstop")
	assert.Equal(t, pgtime.Stored(staleMarker), marker.UTC())
}

// TestClearMarkersOnEligibleStatus_Integration_Idempotent proves this backstop is safe to run on
// every single pass, mirroring ClearCompletedPlaidCleanupMarkers's own idempotency test.
func TestClearMarkersOnEligibleStatus_Integration_Idempotent(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createClearMarkersOnEligibleStatusTestUser(t, pool, repo, "branch1idem", "active")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))

	firstClearedCount, err := repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, firstClearedCount, 1)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.Nil(t, marker)

	_, err = repo.ClearMarkersOnEligibleStatus(ctx)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	assert.Nil(t, marker)
}

// TestPlaidDisconnectRepair_Integration_StaleMarkerHealedBeforeRelink_FreshGraceWindowEnforced is
// THE test the task brief calls "the test that matters": the verified five-step scenario reproduced
// end to end at the repository level (RetentionCleanupCheckWorker.repairPlaidDisconnects itself lives
// in package jobs and cannot be driven from here without an import cycle, so this test drives the
// SAME sequence of repository calls that function makes on every pass — Stamp, then Clear, then the
// candidate query — directly).
//
//  1. A user's repair completed (all linked items disconnected) but the marker-clear retries all
//     failed at the time: marker T survives, stale (older than grace), on a user with ZERO live
//     items. Modeled via seedLinkedItem("disconnected") + setPlaidCleanupPendingAt(T) — raw SQL,
//     because CreateUserTx never sets plaid_cleanup_pending_at and no production writer models
//     "disconnect succeeded but the clear itself failed" as an isolated setup step.
//  2. The NEXT repairPlaidDisconnects pass runs its backstop (this is the fix): calling
//     ClearCompletedPlaidCleanupMarkers here nulls the stale marker, since cleanup for this episode
//     has genuinely completed (zero live items, at least one linked_items row).
//  3. The user relinks a bank — a genuinely NEW live item, modeled via seedLinkedItem("active")
//     (the established convention this entire file already uses for a linked item, since driving a
//     real Plaid Link flow through AccountService would require mocking the Plaid client, out of
//     scope for a repository-layer test).
//  4. The backstop's Stamp half (StampMissingPlaidCleanupMarkers) now fires for this user: marker is
//     NULL, a live item exists, status is ineligible, no active override — so it stamps a FRESH
//     NOW() marker for the new episode. This is what removeUserTierOverrideUpdate's COALESCE would
//     otherwise have to do at comp-removal time; calling Stamp directly here is equivalent to "a comp
//     was never granted at all in this run", which is the simpler and sufficient case to prove the
//     window is closed — admin_user_repository_remove_tier_override_integration_test.go's own suite
//     separately covers the COALESCE write itself.
//  5. GetUsersNeedingPlaidDisconnectRepair, called with the production-shaped grace period, must NOT
//     select this user — the marker is fresh, not past grace. Before this fix (step 2 removed), the
//     marker would still be T (stale), and this same call would WRONGLY select the user, disconnecting
//     a bank connection that is only moments old with zero grace.
//
// RED confirmation (documented, not left implicit): with the ClearCompletedPlaidCleanupMarkers call
// in step 2 removed/no-op'd (simulating pre-fix code, since the method did not exist before this
// change), this test fails — GetUsersNeedingPlaidDisconnectRepair DOES select the user in step 5,
// because the stale marker T (older than grace) survives the relink untouched. Confirmed by running
// this exact scenario with step 2 stubbed out before ClearCompletedPlaidCleanupMarkers was
// implemented; see this task's report for the transcript.
func TestPlaidDisconnectRepair_Integration_StaleMarkerHealedBeforeRelink_FreshGraceWindowEnforced(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_gracebypass_" + suffix,
		Email:              "gracebypass-" + suffix + "@example.com",
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

	// Step 1: repair completed, clear failed — stale marker T survives on a user with zero live items.
	seedLinkedItem(t, pool, user.ID, "item-old-"+suffix, "disconnected")
	staleMarker := time.Now().Add(-testGracePeriod - time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	// Precondition: without any backstop intervention, this user IS currently a "stale marker, zero
	// live items" row — the exact shape assertPlaidMarkerInvariant
	// (profile_repository_plaid_marker_invariant_integration_test.go) already classifies as a
	// violation, and the exact shape this fix must heal.
	total, live := plaidMarkerLinkedItemCounts(t, pool, user.ID)
	require.Equal(t, 1, total)
	require.Equal(t, 0, live)

	// Step 2 (the fix): the backstop's Clear half runs and heals the stale marker BEFORE the relink.
	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	var markerAfterClear *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&markerAfterClear))
	require.Nil(t, markerAfterClear, "precondition for the rest of this test: the stale marker must be healed before the relink")

	// Step 3: the user relinks a bank — a genuinely NEW live item.
	seedLinkedItem(t, pool, user.ID, "item-new-"+suffix, "active")

	// Step 4: the backstop's Stamp half fires on the next pass, arming a FRESH marker for the new
	// episode (equivalent to removeUserTierOverrideUpdate's COALESCE seeing NULL at comp-removal time).
	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	var markerAfterStamp *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&markerAfterStamp))
	require.NotNil(t, markerAfterStamp, "the backstop must arm a fresh marker for the newly-live episode")
	assert.True(t, markerAfterStamp.After(staleMarker), "the fresh marker must NOT be the old stale value T — it must be a new timestamp, giving a full fresh grace window")

	// Step 5: the candidate query, with the production-shaped grace period, must NOT select this user
	// — the marker is fresh, not past grace. This is the assertion that would have FAILED against the
	// stale marker T (older than grace) if step 2 had not healed it first.
	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID,
			"a freshly-relinked bank must get its OWN fresh grace window — it must not inherit a stale, already-expired timestamp from a completed prior episode and be disconnected with zero grace")
	}
}

// TestPlaidDisconnectRepair_Integration_1453ZeroRowsCase_KeepsMarkerAndIsNeverSelected pins the
// #1453 allowance end to end, through both halves of the backstop plus the candidate query: a user
// with ZERO linked_items rows at all must keep whatever marker they have (Clear must not touch it),
// and must never be selected by the candidate query either way (no live item can ever exist for
// them) — the two Plaid-cleanup-marker guarantees this branch makes for that population, proven
// together.
func TestPlaidDisconnectRepair_Integration_1453ZeroRowsCase_KeepsMarkerAndIsNeverSelected(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_1453e2e_" + suffix,
		Email:              "1453e2e-" + suffix + "@example.com",
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

	// Deliberately NO seedLinkedItem call at all — never linked Plaid / migration-residue shape.
	staleMarker := time.Now().Add(-testGracePeriod - time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	_, err = repo.ClearCompletedPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker, "the #1453 allowance: a zero-linked-items user keeps their marker through BOTH backstop halves")
	assert.Equal(t, pgtime.Stored(staleMarker), marker.UTC(), "the marker's value must be completely untouched")

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID, "a user with zero linked_items rows can never have a live item, so the candidate query's own live-item requirement excludes them regardless of marker age")
	}
}

// ─── Issue #1413 Finding 1 (Medium, Codex review, PR #1443 follow-up, headline finding of this
// round): grace must measure actual read-only time, not the marker's raw age ───────────────────────
//
// A comp SUSPENDS Plaid-disconnect eligibility (noActiveManualOverridePredicate excludes the row
// while it's live), but plaid_cleanup_pending_at keeps aging regardless — round-13 deliberately left
// it untouched by a comp grant, so it can already be arbitrarily old by the time a LATER comp lapses.
// Round 10's removal-path anchor (removeUserTierOverrideUpdate, admin_user_repository.go, ROUND
// 19/20) does not reach this at all: no writer runs when a comp simply lapses on its own clock with
// nobody clicking "Remove Override" and the daily expiry job hasn't caught up yet — the marker is
// exactly as stale as whatever cancelled the subscription in the first place, and the ENTIRE 72h
// grace window can elapse while the user still has full comped access, disconnecting them the
// instant the comp lapses with almost none of their OWN grace period spent.
//
// The fix (GetUsersNeedingPlaidDisconnectRepair, profile_repository.go) anchors the grace
// comparison on GREATEST(plaid_cleanup_pending_at, manual_tier_ends_at) instead of
// plaid_cleanup_pending_at alone — the SAME rule removeUserTierOverrideUpdate's own lapse-anchor
// CASE already expresses on the writer side (ROUND 19: anchor on manual_tier_ends_at when it is the
// more recent loss-of-access instant). Both sites now say "grace runs from when access was actually
// lost," from two different angles (a query predicate here, a write-time anchor there) — see each
// site's own doc comment for the cross-reference, so they cannot silently drift apart.

// TestPlaidDisconnectRepair_Integration_LapsedCompRestartsGraceFromLapseNotFromMarker is Finding 1's
// own regression test, reached via the SAME production-writer-only pattern
// TestPlaidDisconnectRepair_Integration_CompGrantedOverAlreadyCancelled_LapsedNeverRemoved_StillRepaired
// established (raw SQL only for the override columns themselves, mirroring
// TestPlaidDisconnectRepair_Integration_ExcludesLiveComp's own convention for that one piece of
// state) — deliberately using the FULL 72h production-shaped grace period, not a near-zero one, so
// the marker's raw age and the lapse's actual age fall on OPPOSITE sides of the grace cutoff.
//
// Scenario:
//  1. plaid_cleanup_pending_at is stamped 100 hours ago — already past the 72h grace window on its
//     own raw age.
//  2. A comp is granted over this already-marked user (round-13: the marker survives, untouched) and
//     lapses just 1 hour ago — well within 72h of actually losing access.
//  3. The expiry job has NOT run — manual_tier_override/manual_tier_ends_at are still on the row,
//     exactly the window Finding 1 exists to close.
//
// Falsifiability: reverting the query to a bare `plaid_cleanup_pending_at < clock_timestamp() -
// make_interval(...)` (dropping GREATEST/manual_tier_ends_at) makes this test fail — the 100-hour-old
// marker alone is already past grace, so the user would be wrongly selected minutes after actually
// losing access.
func TestPlaidDisconnectRepair_Integration_LapsedCompRestartsGraceFromLapseNotFromMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_lapsedgrace_" + suffix,
		Email:              "lapsedgrace-" + suffix + "@example.com",
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
	// Marker is 100 hours old — past the 72h grace window on its raw age alone.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

	// Comp granted, then lapsed just 1 hour ago; the expiry job has not yet run, so the override
	// columns are still present on the row exactly as Finding 1's scenario requires.
	lapsed := time.Now().Add(-1 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, lapsed, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidates {
		assert.NotEqual(t, user.ID, c.ID,
			"a comp that lapsed only 1 hour ago must still have ~71 hours of grace left, even though the marker's own raw timestamp is 100 hours old — grace must measure actual read-only time (since the lapse), not the marker's unrelated raw age")
	}
}

// TestPlaidDisconnectRepair_Integration_LapsedCompOlderThanMarker_UnaffectedByGreatest is the
// brief's fourth reasoned case: a lapsed comp OLDER than the marker. Here manual_tier_ends_at
// predates plaid_cleanup_pending_at, so GREATEST picks the marker (the larger/more recent of the
// two) exactly as the bare, pre-fix comparison always did — this must behave IDENTICALLY to today,
// proving the fix is a strict widening of the anchor (never earlier than the marker), not a
// wholesale replacement of it.
func TestPlaidDisconnectRepair_Integration_LapsedCompOlderThanMarker_UnaffectedByGreatest(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_oldlapse_" + suffix,
		Email:              "oldlapse-" + suffix + "@example.com",
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
	// Marker is 80 hours old — past the 72h grace window on its own, and MORE RECENT than the lapse
	// below (a comp that lapsed long before this episode's marker was even written, e.g. an old
	// comp the expiry job never got around to clearing, unrelated to the CURRENT cancellation).
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-80*time.Hour))
	lapsed := time.Now().Add(-200 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = 'household_2', manual_tier_ends_at = $1 WHERE id = $2`, lapsed, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "when the lapsed comp is OLDER than the marker, GREATEST must pick the marker — behavior must be identical to the pre-fix, marker-only comparison")
}

// TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt
// is Issue #1413 Finding 3's (Medium, Codex review, PR #1443 follow-up) own regression test — the
// fifth, malformed row shape none of the original four GREATEST cases covered: manual_tier_override
// is EMPTY (''), not NULL, so noActiveManualOverridePredicate's own non-empty check
// (`manual_tier_override <> ''`) does NOT exclude this row, yet manual_tier_ends_at is set to a real,
// far-future value — a shape with no DB constraint preventing it, and one this package's own tests
// deliberately exercise independent combinations of (see this file's own header comment and
// GetUsersNeedingPlaidDisconnectRepair's doc comment for the full reasoning).
//
// Without the Finding 3 guard, GREATEST(plaid_cleanup_pending_at, manual_tier_ends_at) would pick up
// the 100-years-out ends_at and defer this user's eligibility for repair indefinitely, even though the
// marker itself is long past the grace window and there is no real, live comp in effect (an empty
// override is not a comp at all). With the guard (manualTierOverrideNonEmptyClause), manual_tier_ends_at
// participates in GREATEST only when the override is genuinely non-empty — here it is not, so
// GREATEST reduces to plaid_cleanup_pending_at alone, and this user must be selected exactly like an
// ordinary user with no override at all.
func TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_malformedgreatest_" + suffix,
		Email:              "malformedgreatest-" + suffix + "@example.com",
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
	// Marker is 100 hours old — well past the 72h grace window on its own.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

	// The malformed shape: override is EMPTY (not NULL), paired with a real, far-future ends_at.
	farFuture := time.Now().AddDate(100, 0, 0)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = '', manual_tier_ends_at = $1 WHERE id = $2`, farFuture, user.ID)
	require.NoError(t, err)

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "an EMPTY override must never let an unrelated manual_tier_ends_at value defer repair eligibility — GREATEST must ignore ends_at here and reduce to the marker alone, exactly as it would for a user with no override at all")
}

// TestStampMissingPlaidCleanupMarkers_Integration_ClockTimestampReflectsWallTimeAcrossLockWait is
// Issue #1413 Finding 2's (Medium, Codex review, PR #1443 follow-up) OWN regression test for
// StampMissingPlaidCleanupMarkers specifically — a separate statement, and a separate CALLER
// (RetentionCleanupCheckWorker.repairPlaidDisconnects, not removeTierOverride), from the
// admin_user_repository.go removal path's own lock-contention test
// (TestRemoveUserTierOverride_Integration_ClockTimestampReflectsWallTimeAcrossLockWait).
//
// The bug this closes: round-20 left StampMissingPlaidCleanupMarkers on NOW(), reasoning that a
// single autocommit UPDATE has no earlier lock-acquiring statement in the same transaction and so
// could not suffer the transaction-start-vs-statement-execution skew the removal path's fix closed.
// That reasoning is wrong: NOW()/transaction_timestamp() is fixed at the (implicit) transaction's
// BEGIN, which for a bare autocommit statement is essentially "when the statement was submitted" —
// BEFORE Postgres acquires the row locks the UPDATE itself needs in order to actually write. If this
// backstop's own WHERE clause happens to match a row a CONCURRENT transaction already holds locked
// (e.g. an admin's RemoveUserTierOverride call, mid-transaction, on that exact row), this UPDATE
// blocks waiting for that lock — and once unblocked, NOW() still reflects the pre-wait instant, not
// the moment the row was actually written. See StampMissingPlaidCleanupMarkers's own doc comment
// (profile_repository.go) for the full reasoning.
//
// Uses waitUntilBlockedBy (admin_user_repository_remove_tier_override_integration_test.go, same
// package) as its synchronization barrier, for the identical Finding 3 reason that test's own doc
// comment explains: a fixed time.Sleep only assumes the goroutine's statement has reached its
// blocked lock wait, and proves neither that it has nor that this test is exercising real
// contention at all.
//
// Falsifiability: reverting StampMissingPlaidCleanupMarkers's SQL from clock_timestamp() back to
// NOW() makes this test fail — the stamped marker would land at (or a few milliseconds after)
// startFence, well BEFORE the lock was actually released, instead of at or after releaseFence.
// Verified by reverting and confirming the assertion below fails reliably across multiple runs, not
// merely once (Issue #1413 Finding 3's own lesson: falsifiability must be checked, not assumed).
func TestStampMissingPlaidCleanupMarkers_Integration_ClockTimestampReflectsWallTimeAcrossLockWait(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_stamplockwait_" + suffix,
		Email:              "stamplockwait-" + suffix + "@example.com",
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

	// Ineligible status, live linked item, NULL marker, no override — exactly the population
	// StampMissingPlaidCleanupMarkers's own WHERE clause targets.
	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")

	// Connection 1: hold a real FOR UPDATE lock on the user row, forcing StampMissingPlaidCleanupMarkers's
	// own UPDATE (below, on a separate connection via a goroutine) to block behind it when it tries
	// to acquire the row lock needed to write this row's plaid_cleanup_pending_at.
	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after Commit below
	var locked int
	require.NoError(t, tx1.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, user.ID).Scan(&locked))

	var tx1PID int32
	require.NoError(t, tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&tx1PID))

	startFence := dbNow(t, pool)

	type result struct {
		healed int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		healed, callErr := repo.StampMissingPlaidCleanupMarkers(ctx)
		done <- result{healed, callErr}
	}()

	// Real barrier (Issue #1413 Finding 3): block here until the goroutine's own backend is
	// CONFIRMED waiting on tx1's lock — not merely assumed to be, after a fixed sleep.
	waitUntilBlockedBy(t, pool, tx1PID, 5*time.Second)

	// Not load-bearing for correctness (waitUntilBlockedBy already proved the block is real) — only
	// widens the assertion margin below.
	const postBlockHoldDuration = 2 * time.Second
	time.Sleep(postBlockHoldDuration)
	releaseFence := dbNow(t, pool)
	require.NoError(t, tx1.Commit(ctx))

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for StampMissingPlaidCleanupMarkers to complete after the lock was released")
	}
	require.NoError(t, res.err)
	require.GreaterOrEqual(t, res.healed, 1, "the backstop must report this user healed")

	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&marker))
	require.NotNil(t, marker)
	assert.False(t, marker.Before(releaseFence),
		"plaid_cleanup_pending_at must reflect statement-execution (wall) time, at or after the lock's release (%v) — got %v, which would mean StampMissingPlaidCleanupMarkers used transaction-start time instead and was backdated by the entire lock wait", releaseFence, *marker)
	assert.True(t, marker.After(startFence.Add(postBlockHoldDuration/2)),
		"plaid_cleanup_pending_at must NOT be anywhere near the backstop's own transaction start (%v) — got %v", startFence, *marker)
}

// TestStampMissingPlaidCleanupMarkers_Integration_DeadlockCycleWithConcurrentAuditActor reproduces
// Issue #1413 Finding 1's (High, Codex review, PR #1443 follow-up) actual cross-user deadlock cycle
// against a real database — see StampMissingPlaidCleanupMarkers's own ROUND 23 doc comment
// (profile_repository.go) for the full mechanism this test proves closed:
//
//  1. User B (an ordinary Stamp candidate — ineligible status, live linked item, NULL marker, no
//     override) gets locked by Stamp's own candidate CTE.
//  2. Stamp's candidate scan (ORDER BY id) then reaches user A, whose row is ALREADY locked by a
//     concurrent transaction shaped exactly like AdminUserRepository.RemoveUserTierOverride's own
//     "prior" CTE (this test runs the REAL removeUserTierOverrideQuery constant, not a hand-copied
//     restatement, so it cannot silently drift from production). Stamp blocks, waiting for that
//     transaction to release A.
//  3. That transaction, STILL holding A (uncommitted), inserts an audit_logs row whose actor
//     (user_id) is B — exactly as admin_user_repository.go's removeTierOverride does immediately
//     after its own CTE+UPDATE succeeds, and before it commits. B need not have an active
//     subscription to be a valid audit actor (see StampMissingPlaidCleanupMarkers's own doc comment
//     for why that is a separate, correct contract, not something to "fix").
//  4. The audit insert's FK to users(id) requests a FOR KEY SHARE lock on B.
//
// With the Finding 1 fix (FOR NO KEY UPDATE, not FOR UPDATE, on Stamp's own candidate lock), step 4
// does NOT conflict with Stamp's hold on B, so the audit insert completes immediately — proven below
// by asserting it completes well under Postgres's default 1-second deadlock_timeout, not merely that
// it eventually succeeds (a slow success could still mean one side lost a real deadlock and the OTHER
// side happened to be the survivor, only proven by timing, never by the error return alone).
//
// Falsifiability (manually verified, not automated in this test): reverting
// StampMissingPlaidCleanupMarkers's candidate CTE from FOR NO KEY UPDATE back to FOR UPDATE makes
// this exact scenario deadlock — PostgreSQL's deadlock detector aborts one of the two transactions
// (which side is arbitrary) after ~1 second, so EITHER the audit INSERT below returns a "deadlock
// detected" error, OR StampMissingPlaidCleanupMarkers's own call does, and either way the fast-path
// timing assertion fails. Verified by temporarily reverting the lock mode and re-running this test
// several times — see this round's report for the exact count and outcome.
//
// Uses waitUntilBlockedBy (admin_user_repository_remove_tier_override_integration_test.go, same
// package) as its synchronization barrier — Issue #1413 Finding 4 (Low, Codex review, PR #1443
// follow-up): the barrier must identify the SPECIFIC backend under test (here, the goroutine running
// StampMissingPlaidCleanupMarkers, confirmed blocked specifically on this test's own tx2 backend PID),
// never merely "some backend is in a lock wait somewhere" — an unrelated concurrent test's lock wait
// must not be able to satisfy this barrier.
func TestStampMissingPlaidCleanupMarkers_Integration_DeadlockCycleWithConcurrentAuditActor(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Two ordinary Stamp candidates: ineligible status, live linked item, NULL marker, no override.
	// idA must sort AFTER idB so Stamp's own `ORDER BY id` locks B first, then reaches A (already
	// held by the concurrent "removal" transaction below) — reproducing the brief's exact ordering.
	idA := uuid.New().String()
	idB := uuid.New().String()
	if idA < idB {
		idA, idB = idB, idA
	}
	require.True(t, idB < idA, "test setup invariant: idB must sort before idA")

	userA := &domain.User{
		ID:                 idA,
		ClerkUserID:        "clerk_deadlock_a_" + suffix,
		Email:              "deadlock-a-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	userB := &domain.User{
		ID:                 idB,
		ClerkUserID:        "clerk_deadlock_b_" + suffix,
		Email:              "deadlock-b-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, userA))
	require.NoError(t, repo.CreateUserTx(ctx, tx, userB))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE resource_id = $1", userA.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM linked_items WHERE user_id = ANY($1)", []string{userA.ID, userB.ID})
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = ANY($1)", []string{userA.ID, userB.ID})
	})

	seedLinkedItem(t, pool, userA.ID, "item-a-"+suffix, "active")
	seedLinkedItem(t, pool, userB.ID, "item-b-"+suffix, "active")

	// tx2 simulates RemoveUserTierOverride's own transaction shape: run the REAL production query
	// (removeUserTierOverrideQuery, admin_user_repository.go) against A — this acquires A's row lock
	// via that query's own "prior" CTE FOR UPDATE, exactly as production does, whether or not A
	// actually carries an override to clear (it does not here; the lock is acquired regardless, and
	// RemoveUserTierOverride is a safe no-op for a user with none — see removeTierOverride's own doc
	// comment). Deliberately NOT committed yet — the real removeTierOverride doesn't commit until
	// AFTER its own audit insert, which is exactly the window this test needs to control.
	tx2, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx2.Rollback(ctx) //nolint:errcheck // no-op after Commit below
	var revertedTier, revertedStatus, clerkUserIDEnc string
	var priorOverride *string
	// Issue #1413 Task NB: removeUserTierOverrideUpdate's RETURNING clause now also returns
	// prior.manual_tier_ends_at (additive — this test's deadlock/lock-ordering assertions below
	// are unaffected). Scanned into an otherwise-unused destination since this test doesn't
	// assert on it.
	var priorEndsAt *time.Time
	require.NoError(t, tx2.QueryRow(ctx, removeUserTierOverrideQuery, userA.ID, testRetentionMonths).Scan(
		&revertedTier, &revertedStatus, &clerkUserIDEnc, &priorOverride, &priorEndsAt,
	))
	assert.Nil(t, priorOverride, "test setup invariant: A must have no override to remove")

	var tx2PID int32
	require.NoError(t, tx2.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&tx2PID))

	type stampResult struct {
		healed int
		err    error
	}
	done := make(chan stampResult, 1)
	go func() {
		healed, callErr := repo.StampMissingPlaidCleanupMarkers(ctx)
		done <- stampResult{healed, callErr}
	}()

	// Real barrier (Finding 4): block until Stamp's own backend is CONFIRMED waiting on tx2's lock —
	// i.e. Stamp has already locked B (via its own candidate CTE) and is now blocked reaching A.
	waitUntilBlockedBy(t, pool, tx2PID, 5*time.Second)

	// The moment of truth: tx2, STILL holding A, inserts an audit row whose actor is B — exactly
	// like removeTierOverride's own audit insert, immediately after its CTE+UPDATE and before commit.
	auditStart := time.Now()
	_, err = tx2.Exec(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'remove_manual_tier', 'user', $2, NULL)
	`, userB.ID, userA.ID)
	auditElapsed := time.Since(auditStart)
	require.NoError(t, err, "the audit insert referencing B as actor must not fail — with the Finding 1 fix, FOR NO KEY UPDATE on B does not conflict with the audit FK's FOR KEY SHARE request")
	assert.Less(t, auditElapsed, 500*time.Millisecond,
		"the audit insert must complete immediately, not after waiting anywhere near Postgres's 1s deadlock_timeout — a slow-but-successful insert would mean the OTHER side (Stamp) lost a real deadlock instead of this cycle being genuinely closed; got %v", auditElapsed)

	require.NoError(t, tx2.Commit(ctx))

	var res stampResult
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for StampMissingPlaidCleanupMarkers to complete after tx2 committed — the deadlock cycle may not be closed")
	}
	require.NoError(t, res.err, "StampMissingPlaidCleanupMarkers must not receive a deadlock-detected error")
	assert.GreaterOrEqual(t, res.healed, 1, "at least B must be healed")

	var markerA, markerB *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userA.ID).Scan(&markerA))
	require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userB.ID).Scan(&markerB))
	assert.NotNil(t, markerB, "B must be healed by Stamp")
	assert.NotNil(t, markerA, "A must also be healed by Stamp once tx2 released it — A never stopped qualifying (no override was ever present to clear)")
}
