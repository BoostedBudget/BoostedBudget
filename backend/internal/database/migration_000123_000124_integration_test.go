//go:build integration

package database

// Integration tests for migrations 000123 (backfill_linked_items_null_status) and 000124
// (add_linked_items_status_not_null) — Issue #1444.
//
// linked_items.status has been nullable since migration 000013 added it with a DEFAULT 'active'
// but no NOT NULL constraint. Since PostgreSQL 11, `ADD COLUMN ... DEFAULT` backfills every
// pre-existing row at ALTER time, and the only production writer (linked_item_repository.go's
// Create) omits `status` from its column list entirely, always getting the column default — so a
// NULL status is unreachable through any production write path today. 000123's backfill is
// therefore expected to affect 0 rows against a normal database; these tests seed a genuine NULL
// via raw SQL (bypassing the repository layer entirely) to exercise it honestly.
//
// Each scenario re-applies the real up SQL (read from the migration files, not a hand-copied
// restatement) inside a rolled-back transaction, mirroring migration_000100_integration_test.go's
// established pattern in this same package. Any scenario that needs to seed a genuinely NULL
// status first calls simulatePreMigration000124Schema to transactionally DROP the NOT NULL
// constraint — required once 000124 has been applied for real against the target database (as it
// normally would be, including this test's own target once this PR merges), since a NULL status
// can no longer be constructed at all otherwise.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run 'TestMigration000123|TestMigration000124' -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyMigration000123 runs the real 000123 up SQL (backfill) in tx.
func applyMigration000123(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000123_backfill_linked_items_null_status.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000123 failed")
}

// applyMigration000124 runs the real 000124 up SQL (ALTER ... SET NOT NULL) in tx.
func applyMigration000124(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000124_add_linked_items_status_not_null.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000124 failed")
}

// seedMig123User inserts a minimal users row, returning its UUID. linked_items.user_id has a NOT
// NULL FK to users, so every linked_items fixture below needs one.
func seedMig123User(t *testing.T, tx pgx.Tx, suffix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|mig123-"+suffix+"-"+userID, "mig123-"+suffix+"-"+userID+"@example.com")
	require.NoError(t, err)
	return userID
}

// seedMig123LinkedItem inserts a linked_items row with the given status (nil for a genuinely NULL
// column value — status has no schema default enforced at the application layer here since it is
// passed explicitly), returning its UUID.
func seedMig123LinkedItem(t *testing.T, tx pgx.Tx, userID, suffix string, status *string) string {
	t.Helper()
	itemID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, itemID, userID, "mig123-item-"+suffix+"-"+itemID, status)
	require.NoError(t, err)
	return itemID
}

func getMig123Status(t *testing.T, tx pgx.Tx, itemID string) *string {
	t.Helper()
	var status *string
	require.NoError(t, tx.QueryRow(context.Background(),
		"SELECT status FROM linked_items WHERE id = $1", itemID).Scan(&status))
	return status
}

// simulatePreMigration000124Schema transactionally DROPs the NOT NULL constraint 000124 adds, so
// a scenario can seed a genuinely NULL status and exercise 000123/124 as if run against a database
// that had not yet applied them — exactly the state these migrations are designed for. Once 000124
// has been applied for real (any migrated database, including this test's own target — see this
// file's header comment), a NULL status can no longer be constructed at all without this step.
// Rolled back with the rest of the transaction; the real schema is never permanently altered.
func simulatePreMigration000124Schema(t *testing.T, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(context.Background(), "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)
}

func TestMigration000123_BackfillLinkedItemsNullStatus(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario A: a genuinely NULL status is backfilled to 'active'. ───────────────────────
	t.Run("ScenarioA_NullStatusBackfilledToActive", func(t *testing.T) {
		ctx := context.Background()
		suffix := "A-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreMigration000124Schema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, nil)

		got := getMig123Status(t, tx, itemID)
		require.Nil(t, got, "sanity check: the seeded row must genuinely be NULL before the migration runs")

		applyMigration000123(t, tx)

		got = getMig123Status(t, tx, itemID)
		require.NotNil(t, got, "a NULL status must be backfilled, not left NULL")
		assert.Equal(t, "active", *got)
	})

	// ─── Scenario B: a non-NULL status (e.g. 'disconnected') must be left completely
	// untouched — the migration must never overwrite a genuine, already-set status. ────────────
	t.Run("ScenarioB_NonNullStatusUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := "B-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		userID := seedMig123User(t, tx, suffix)
		disconnected := "disconnected"
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, &disconnected)

		applyMigration000123(t, tx)

		got := getMig123Status(t, tx, itemID)
		require.NotNil(t, got)
		assert.Equal(t, "disconnected", *got, "a non-NULL status must not be overwritten by the backfill")
	})

	// ─── Scenario C: idempotency — running the up SQL twice must not error, and a row already
	// backfilled by the first pass must not be touched again by the second. ───────────────────
	t.Run("ScenarioC_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()
		suffix := "C-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreMigration000124Schema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, nil)

		applyMigration000123(t, tx)
		firstPass := getMig123Status(t, tx, itemID)
		require.NotNil(t, firstPass)
		assert.Equal(t, "active", *firstPass)

		applyMigration000123(t, tx) // re-apply — must not error

		secondPass := getMig123Status(t, tx, itemID)
		require.NotNil(t, secondPass)
		assert.Equal(t, "active", *secondPass, "re-applying must not change an already-backfilled row")
	})
}

func TestMigration000124_AddLinkedItemsStatusNotNull(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario D: after 000123's backfill, 000124's ALTER succeeds (no NULL rows remain),
	// and a subsequent attempt to insert an explicit NULL status is rejected by the new
	// constraint — proving the NOT NULL is genuinely enforced, not just a no-op ALTER. ─────────
	t.Run("ScenarioD_ConstraintEnforcedAfterBackfill", func(t *testing.T) {
		ctx := context.Background()
		suffix := "D-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreMigration000124Schema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, nil)

		applyMigration000123(t, tx)
		applyMigration000124(t, tx)

		got := getMig123Status(t, tx, itemID)
		require.NotNil(t, got)
		assert.Equal(t, "active", *got)

		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', NULL)
		`, userID, "mig123-item-"+suffix+"-post-notnull")
		require.Error(t, err, "inserting an explicit NULL status must be rejected once 000124 has run")
		assert.Contains(t, err.Error(), "null value in column \"status\"")
	})

	// ─── Scenario E: idempotency — running 000124's up SQL twice must not error. ───────────────
	t.Run("ScenarioE_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		applyMigration000123(t, tx)
		applyMigration000124(t, tx)
		applyMigration000124(t, tx) // re-apply — must not error (column already NOT NULL)
	})
}
