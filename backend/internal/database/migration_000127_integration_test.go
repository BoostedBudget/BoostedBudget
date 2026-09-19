//go:build integration

package database

// Integration tests for migration 000127 (backfill_malformed_tier_override_and_marker_residue) —
// Issue #1453.
//
// Two independent backfills in one migration:
//
//  1. Nulls manual_tier_override (and manual_tier_ends_at alongside it) for any user whose override
//     value does not name a currently-configured tier (premium/household_2/household_3/household_4/
//     household_5) — the class PR #1443 made harmless at every read path; this makes it unreachable
//     by clearing existing occurrences (the write-time validation that used to guard new writes was
//     deleted in #1652, which is why this one-time cleanup — plus the read-time validator and the
//     Issue #1453 monitoring gauge — remains the control).
//  2. Nulls plaid_cleanup_pending_at for any inactive/cancelled user with ZERO linked_items rows at
//     all — the residue migrations 000119/000120 left behind by stamping every eligible user
//     regardless of whether they ever linked a single Plaid item. ClearCompletedPlaidCleanupMarkers
//     (profile_repository.go) never touches this population (its anyLinkedItemExistsClause guard
//     deliberately excludes zero-row users — the #1453 allowance), so nothing else in this codebase
//     would ever clear it.
//
// Each scenario re-applies the real 000127 up SQL (read from the migration file, not a hand-copied
// restatement) inside a rolled-back transaction — mirrors migration_000120_integration_test.go's
// established pattern in this same package.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000127 -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyMigration000127 runs the real 000127 up SQL in tx — reading from the actual file, not a
// hand-copied restatement, so this test exercises the production SQL verbatim.
func applyMigration000127(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000127_backfill_malformed_tier_override_and_marker_residue.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000127 failed")
}

// seedMig127User inserts a minimal users row with the given subscription_status, manual tier
// override, and plaid_cleanup_pending_at marker, returning its UUID.
func seedMig127User(
	t *testing.T, tx pgx.Tx, suffix, status string,
	manualOverride *string, manualEndsAt *time.Time,
	pendingAt *time.Time,
) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (
			id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
			manual_tier_override, manual_tier_ends_at, plaid_cleanup_pending_at
		)
		VALUES ($1, $2, $3, 'premium', $4, 'iad', $5, $6, $7)
	`, userID, "test|mig127-"+suffix+"-"+userID, "mig127-"+suffix+"-"+userID+"@example.com",
		status, manualOverride, manualEndsAt, pendingAt)
	require.NoError(t, err)
	return userID
}

func getMig127UserState(t *testing.T, tx pgx.Tx, userID string) (override, pendingAt *string) {
	t.Helper()
	var ov *string
	var pa *time.Time
	require.NoError(t, tx.QueryRow(context.Background(),
		"SELECT manual_tier_override, plaid_cleanup_pending_at FROM users WHERE id = $1", userID,
	).Scan(&ov, &pa))
	var paStr *string
	if pa != nil {
		s := pa.String()
		paStr = &s
	}
	return ov, paStr
}

func TestMigration000127_BackfillMalformedTierOverrideAndMarkerResidue(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Part 1 scenarios: malformed manual_tier_override ─────────────────────────────────────

	// ─── Scenario A: a manual_tier_override naming no configured tier must be nulled, along with
	// manual_tier_ends_at (an end date paired with no override is meaningless). ────────────────────
	t.Run("ScenarioA_MalformedOverrideNulledWithEndsAt", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("A-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		malformed := "not_a_tier"
		futureEnd := time.Now().Add(24 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", &malformed, &futureEnd, nil)

		applyMigration000127(t, tx)

		var override *string
		var endsAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1", userID).Scan(&override, &endsAt))
		assert.Nil(t, override, "a manual_tier_override naming no configured tier must be nulled")
		assert.Nil(t, endsAt, "manual_tier_ends_at must be cleared alongside a malformed override")
	})

	// ─── Scenario B: a manual_tier_override naming a REAL configured tier must be left untouched. ──
	t.Run("ScenarioB_ValidOverrideUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("B-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		valid := "household_3"
		futureEnd := time.Now().Add(24 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", &valid, &futureEnd, nil)

		applyMigration000127(t, tx)

		var override *string
		var endsAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1", userID).Scan(&override, &endsAt))
		require.NotNil(t, override)
		assert.Equal(t, "household_3", *override, "a validly-configured override must be left untouched")
		require.NotNil(t, endsAt)
	})

	// ─── Scenario C: a NULL manual_tier_override must be left NULL (no-op, no error). ──────────────
	t.Run("ScenarioC_NullOverrideUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("C-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		userID := seedMig127User(t, tx, suffix, "cancelled", nil, nil, nil)

		applyMigration000127(t, tx)

		var override *string
		require.NoError(t, tx.QueryRow(ctx, "SELECT manual_tier_override FROM users WHERE id = $1", userID).Scan(&override))
		assert.Nil(t, override)
	})

	// ─── Part 2 scenarios: plaid_cleanup_pending_at residue ────────────────────────────────────

	// ─── Scenario D: an inactive/cancelled user with a marker but ZERO linked_items rows at all
	// must have the marker nulled — the 000119/000120 residue this migration exists to clean up. ───
	t.Run("ScenarioD_ZeroLinkedItemsResidueMarkerNulled", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("D-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		staleMarker := time.Now().Add(-1000 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", nil, nil, &staleMarker)

		applyMigration000127(t, tx)

		var pendingAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
		assert.Nil(t, pendingAt, "a marker on a user with ZERO linked_items rows at all must be nulled — the #1453 residue cleanup")
	})

	// ─── Scenario D2: same as D, but with a PENDING DELETION. The marker must STILL be nulled.
	//
	// This scenario's absence is why a real gap shipped, caught by a Codex adversarial review: an
	// earlier draft of 000127 carried ClearCompletedPlaidCleanupMarkers's `deletion_requested_at IS
	// NULL` guard across to this migration, which left exactly this population stamped forever and
	// made the migration's own documented invariant ("the remaining non-NULL population is always
	// backed by at least one genuine linked_items row") false.
	//
	// The guard does not transfer, because the two statements exclude deletion-pending users for
	// different reasons: ClearCompletedPlaidCleanupMarkers does it because the deletion flow owns
	// Plaid cleanup during the 30-day grace (the #1449 H6 split) — runtime candidate selection —
	// whereas this migration corrects a historical artifact, and a zero-linked-items user has
	// nothing to clean up regardless of deletion state. Nulling cannot race the deletion flow:
	// neither DeletionService nor AccountDeletionWorker reads plaid_cleanup_pending_at at all.
	//
	// Note ClearMarkersOnEligibleStatus would not rescue these rows either — its predicate is
	// NOT(inactive/cancelled), and these users ARE inactive/cancelled. ────────────────────────────
	t.Run("ScenarioD2_ZeroLinkedItemsResidueNulledEvenWithPendingDeletion", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("D2-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		staleMarker := time.Now().Add(-1000 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", nil, nil, &staleMarker)

		// Put the user into the 30-day deletion grace window.
		deletionRequestedAt := time.Now().Add(-24 * time.Hour)
		_, err = tx.Exec(ctx,
			`UPDATE users SET deletion_requested_at = $1, deletion_scheduled_at = $2 WHERE id = $3`,
			deletionRequestedAt, deletionRequestedAt.Add(30*24*time.Hour), userID)
		require.NoError(t, err)

		// Sanity-check the fixture actually reached the state under test — otherwise this scenario
		// could pass for the trivial reason that no deletion was pending after all.
		var requestedAt *time.Time
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT deletion_requested_at FROM users WHERE id = $1", userID).Scan(&requestedAt))
		require.NotNil(t, requestedAt, "fixture precondition: the user must have a pending deletion")

		applyMigration000127(t, tx)

		var pendingAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
		assert.Nil(t, pendingAt,
			"a zero-linked-items residue marker must be nulled even while a deletion is pending — "+
				"otherwise this migration's own documented invariant is false for that population")

		// The deletion itself must be left strictly alone.
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT deletion_requested_at FROM users WHERE id = $1", userID).Scan(&requestedAt))
		require.NotNil(t, requestedAt, "the migration must not disturb a pending deletion")
		assert.True(t, deletionRequestedAt.Equal(*requestedAt))
	})

	// ─── Scenario E: a user with AT LEAST ONE linked_items row (even a disconnected one) must be
	// left COMPLETELY untouched — that population belongs to ClearCompletedPlaidCleanupMarkers's
	// ongoing runtime responsibility, not this one-time migration. ─────────────────────────────────
	t.Run("ScenarioE_AtLeastOneLinkedItemUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("E-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		staleMarker := time.Now().Add(-1000 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", nil, nil, &staleMarker)
		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'disconnected')
		`, userID, "item-"+suffix)
		require.NoError(t, err)

		applyMigration000127(t, tx)

		var pendingAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
		require.NotNil(t, pendingAt, "a user with at least one linked_items row must not be touched by this one-time migration")
		assert.True(t, staleMarker.Equal(*pendingAt))
	})

	// ─── Scenario F: an ACTIVE-status user's marker must never be touched, regardless of
	// linked_items state. ───────────────────────────────────────────────────────────────────────────
	t.Run("ScenarioF_ActiveUserMarkerUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("F-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		staleMarker := time.Now().Add(-1000 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "active", nil, nil, &staleMarker)

		applyMigration000127(t, tx)

		var pendingAt *time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
		require.NotNil(t, pendingAt, "an active-status user must never be touched by this migration")
		assert.True(t, staleMarker.Equal(*pendingAt))
	})

	// ─── Scenario G: idempotency — running the up SQL twice must not error and must leave an
	// already-cleaned row unchanged the second time. ────────────────────────────────────────────────
	t.Run("ScenarioG_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("G-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		malformed := "not_a_tier"
		staleMarker := time.Now().Add(-1000 * time.Hour)
		userID := seedMig127User(t, tx, suffix, "cancelled", &malformed, nil, &staleMarker)

		applyMigration000127(t, tx)
		firstOverride, firstMarker := getMig127UserState(t, tx, userID)
		assert.Nil(t, firstOverride)
		assert.Nil(t, firstMarker)

		applyMigration000127(t, tx) // re-apply — must not error
		secondOverride, secondMarker := getMig127UserState(t, tx, userID)
		assert.Nil(t, secondOverride)
		assert.Nil(t, secondMarker)
	})

	// ─── Scenario H (documents the "no production users yet" premise): asserts that, against a
	// database with only THIS test's own fixtures visible in the transaction, an ordinary
	// well-formed user seeded with no override and no marker at all is completely unaffected and
	// the migration runs without error even when there is nothing to backfill. ─────────────────────
	t.Run("ScenarioH_NoOpOnOrdinaryUser", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("H-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		userID := seedMig127User(t, tx, suffix, "active", nil, nil, nil)

		applyMigration000127(t, tx)

		override, pendingAt := getMig127UserState(t, tx, userID)
		assert.Nil(t, override)
		assert.Nil(t, pendingAt)
	})
}
