//go:build integration

package database

// Integration tests for migrations 000123-000126 — Issue #1444, the linked_items.status NOT NULL
// transition.
//
// linked_items.status has been nullable since migration 000013 added it with a DEFAULT 'active' but
// no NOT NULL constraint. Since PostgreSQL 11, `ADD COLUMN ... DEFAULT` backfills every pre-existing
// row at ALTER time, and the only production writer (linked_item_repository.go's Create) omits
// `status` from its column list entirely, always getting the column default — so a NULL status is
// unreachable through any production write path today. 000123's backfill is therefore expected to
// affect 0 rows against a normal database; these tests seed a genuine NULL via raw SQL (bypassing
// the repository layer entirely) to exercise it honestly.
//
// THE TRANSITION IS FOUR MIGRATIONS, NOT TWO. A bare `SET NOT NULL` performs a mandatory full-table
// validation scan while holding ACCESS EXCLUSIVE, blocking all reads and writes. An earlier draft of
// this work did that; a Codex adversarial review caught it, and it also violated a convention this
// same branch introduced (#1464). The split is:
//
//	000123  backfill NULL -> 'active'
//	000124  ADD CONSTRAINT ... CHECK (status IS NOT NULL) NOT VALID   (instant, no scan)
//	000125  VALIDATE CONSTRAINT                                      (scans under SHARE UPDATE
//	                                                                  EXCLUSIVE — does NOT block
//	                                                                  concurrent reads or writes)
//	000126  SET NOT NULL (PG12+ skips the scan, proving it from the validated CHECK) + DROP the
//	        now-redundant CHECK
//
// Verified empirically while writing these migrations: pg_locks reports ShareUpdateExclusiveLock
// during 000125's VALIDATE and a concurrent SELECT completes, whereas the same SELECT times out
// against a held ACCESS EXCLUSIVE lock.
//
// Each scenario re-applies the real up SQL (read from the migration files, not a hand-copied
// restatement) inside a rolled-back transaction, mirroring migration_000100_integration_test.go's
// established pattern in this same package. Any scenario that needs to seed a genuinely NULL status
// first calls simulatePreNotNullSchema to transactionally undo the constraints — required once these
// migrations have been applied for real against the target database (as they normally would be,
// including this test's own target once this PR merges), since a NULL status can no longer be
// constructed at all otherwise.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run 'TestMigration00012[3456]' -tags integration -v

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

// applyMigration000124 runs the real 000124 up SQL (ADD CHECK ... NOT VALID) in tx.
func applyMigration000124(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000124_add_linked_items_status_not_null.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000124 failed")
}

// applyMigration000125 runs the real 000125 up SQL (VALIDATE CONSTRAINT) in tx. Returns the error
// rather than asserting on it: one scenario below deliberately exercises the failure path, which is
// this migration's fail-closed guarantee.
func applyMigration000125(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	upSQL := readMigrationFile(t, "000125_validate_linked_items_status_not_null.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	return err
}

// applyMigration000126 runs the real 000126 up SQL (SET NOT NULL + DROP the scaffolding CHECK) in tx.
func applyMigration000126(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000126_set_linked_items_status_not_null.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000126 failed")
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
// column value — status is passed explicitly, so the column default does not apply), returning its
// UUID.
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

// statusColumnIsNullable reports information_schema's view of linked_items.status.
func statusColumnIsNullable(t *testing.T, tx pgx.Tx) bool {
	t.Helper()
	var isNullable string
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'linked_items' AND column_name = 'status'
	`).Scan(&isNullable))
	return isNullable == "YES"
}

// notNullCheckState returns whether the scaffolding CHECK exists and, if so, whether it is validated.
func notNullCheckState(t *testing.T, tx pgx.Tx) (exists bool, validated bool) {
	t.Helper()
	rows, err := tx.Query(context.Background(), `
		SELECT convalidated FROM pg_constraint
		WHERE conrelid = 'linked_items'::regclass AND conname = 'linked_items_status_not_null'
	`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		require.NoError(t, rows.Scan(&validated))
		exists = true
	}
	require.NoError(t, rows.Err())
	return exists, validated
}

// simulatePreNotNullSchema transactionally undoes whatever parts of the 000124-000126 transition the
// target database has already applied, so a scenario can seed a genuinely NULL status and exercise
// the migrations as if run against a database that had not yet applied them — exactly the state they
// are designed for. Rolled back with the rest of the transaction; the real schema is never
// permanently altered.
//
// Both steps are needed. Dropping NOT NULL alone is insufficient if the scaffolding CHECK is still
// present, because a NOT VALID CHECK still rejects NEW rows (NOT VALID only exempts rows that
// already existed when it was added) — so an insert of NULL would fail on the CHECK instead.
func simulatePreNotNullSchema(t *testing.T, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(context.Background(), "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(),
		"ALTER TABLE linked_items DROP CONSTRAINT IF EXISTS linked_items_status_not_null")
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
		simulatePreNotNullSchema(t, tx)

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
		simulatePreNotNullSchema(t, tx)

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

func TestMigration000124_AddNotValidCheck(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario D: 000124 adds the CHECK as NOT VALID and does NOT make the column NOT NULL.
	// That distinction is the whole point of the split — the column only becomes NOT NULL in
	// 000126, after 000125 has done the scan under a non-blocking lock. ────────────────────────
	t.Run("ScenarioD_AddsUnvalidatedCheckWithoutSettingNotNull", func(t *testing.T) {
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		applyMigration000124(t, tx)

		exists, validated := notNullCheckState(t, tx)
		require.True(t, exists, "000124 must create the linked_items_status_not_null CHECK")
		assert.False(t, validated,
			"000124's CHECK must be NOT VALID — a validated CHECK would mean it scanned the table "+
				"under ACCESS EXCLUSIVE, which is exactly what this split exists to avoid")
		assert.True(t, statusColumnIsNullable(t, tx),
			"000124 must NOT set NOT NULL; that is 000126's job, after 000125's non-blocking scan")
	})

	// ─── Scenario E: a NOT VALID CHECK still rejects NEW rows. NOT VALID exempts only rows that
	// already existed when the constraint was added, so from 000124 onward no fresh NULL can be
	// introduced even though the column is still formally nullable. ───────────────────────────
	t.Run("ScenarioE_NotValidCheckStillRejectsNewNulls", func(t *testing.T) {
		ctx := context.Background()
		suffix := "E-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		applyMigration000124(t, tx)

		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', NULL)
		`, userID, "mig123-item-"+suffix+"-post-check")
		require.Error(t, err, "a NOT VALID CHECK must still reject a newly inserted NULL status")
		assert.Contains(t, err.Error(), "linked_items_status_not_null")
	})

	// ─── Scenario F: idempotency — running 000124's up SQL twice must not error. ───────────────
	t.Run("ScenarioF_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		applyMigration000124(t, tx)
		applyMigration000124(t, tx) // re-apply — must not error (constraint already present)
	})
}

func TestMigration000125_ValidateCheck(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario G: after 000123's backfill, validation succeeds and marks the CHECK valid. ──
	t.Run("ScenarioG_ValidatesAfterBackfill", func(t *testing.T) {
		ctx := context.Background()
		suffix := "G-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		seedMig123LinkedItem(t, tx, userID, suffix, nil)

		applyMigration000123(t, tx)
		applyMigration000124(t, tx)
		require.NoError(t, applyMigration000125(t, tx))

		exists, validated := notNullCheckState(t, tx)
		require.True(t, exists)
		assert.True(t, validated, "000125 must leave the CHECK validated")
		assert.True(t, statusColumnIsNullable(t, tx),
			"000125 validates only; the column must still be nullable until 000126")
	})

	// ─── Scenario H (the fail-closed guarantee): if a NULL row somehow survives the backfill,
	// 000125 must FAIL rather than silently proceed. This is the scenario that makes the
	// three-step split safe — the deploy stops here, with the column still nullable and the
	// constraint un-validated, which is a coherent state the application still runs against.
	//
	// The NULL is seeded BEFORE 000124 so that NOT VALID genuinely exempts it from the initial
	// add; 000123 is deliberately skipped so the row survives to be caught by the scan. ───────
	t.Run("ScenarioH_FailsClosedWhenNullSurvives", func(t *testing.T) {
		ctx := context.Background()
		suffix := "H-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, nil)
		require.Nil(t, getMig123Status(t, tx, itemID), "sanity check: the row must genuinely be NULL")

		applyMigration000124(t, tx) // NOT VALID — exempts the pre-existing NULL row

		err = applyMigration000125(t, tx)
		require.Error(t, err,
			"000125 must fail loudly when a NULL row survived the backfill — silently proceeding "+
				"would let 000126 set NOT NULL on data that violates it")
		assert.Contains(t, err.Error(), "linked_items_status_not_null")
	})
}

func TestMigration000126_SetNotNull(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario I: 000126 sets NOT NULL and drops the now-redundant scaffolding CHECK, and the
	// constraint is genuinely enforced afterwards (not a no-op ALTER). ─────────────────────────
	t.Run("ScenarioI_SetsNotNullAndDropsScaffoldingCheck", func(t *testing.T) {
		ctx := context.Background()
		suffix := "I-" + uuid.New().String()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		userID := seedMig123User(t, tx, suffix)
		itemID := seedMig123LinkedItem(t, tx, userID, suffix, nil)

		applyMigration000123(t, tx)
		applyMigration000124(t, tx)
		require.NoError(t, applyMigration000125(t, tx))
		applyMigration000126(t, tx)

		got := getMig123Status(t, tx, itemID)
		require.NotNil(t, got)
		assert.Equal(t, "active", *got)

		assert.False(t, statusColumnIsNullable(t, tx), "000126 must set the column NOT NULL")
		exists, _ := notNullCheckState(t, tx)
		assert.False(t, exists,
			"000126 must drop the scaffolding CHECK — leaving it would make every future INSERT "+
				"evaluate the same predicate twice")

		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', NULL)
		`, userID, "mig123-item-"+suffix+"-post-notnull")
		require.Error(t, err, "inserting an explicit NULL status must be rejected once 000126 has run")
		assert.Contains(t, err.Error(), "null value in column \"status\"",
			"the rejection must now come from the column's own NOT NULL, not from the dropped CHECK")
	})

	// ─── Scenario J: idempotency — running 000126's up SQL twice must not error. ───────────────
	t.Run("ScenarioJ_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		simulatePreNotNullSchema(t, tx)

		applyMigration000123(t, tx)
		applyMigration000124(t, tx)
		require.NoError(t, applyMigration000125(t, tx))
		applyMigration000126(t, tx)
		applyMigration000126(t, tx) // re-apply — must not error
	})
}
