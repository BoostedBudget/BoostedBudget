//go:build integration

package database

// Integration tests for migration 000100 (user_accounts blind-index dedup + unique index).
//
// These tests require a real Postgres database with all migrations applied.
// Each scenario rolls back migration 000100 within a transaction, seeds test data,
// re-applies the migration SQL, asserts the expected state, and rolls back to clean up.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000100 -tags integration -v

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Connection helper ───────────────────────────────────────────────────────

func connectMigrationTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping migration integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// ─── SQL file readers ────────────────────────────────────────────────────────

// migrationDir returns the absolute path to backend/migrations/ by walking up
// from the test file's location. go test sets CWD to the package directory, so
// the relative path "../../migrations" is stable under `go test ./internal/database/...`.
func migrationDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// thisFile = .../backend/internal/database/migration_000100_integration_test.go
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// readMigrationFile reads a migration file from backend/migrations/.
func readMigrationFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(migrationDir(t), name))
	require.NoError(t, err, "failed to read migration file %s", name)
	return string(b)
}

// migration000100PreflightSQL is the Step 1 preflight-only DO block.
// Used by Scenario D to exercise just the preflight check in isolation.
// Must match the actual Step 1 block in 000100...up.sql.
const migration000100PreflightSQL = `
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM user_accounts WHERE institution_id_blind_idx IS NULL) THEN
    RAISE EXCEPTION '#1334: un-backfilled user_accounts (NULL institution_id_blind_idx); run cmd/encrypt-migrate --table user_accounts first';
  END IF;
  IF EXISTS (
    SELECT 1 FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
      AND mask IS NOT NULL
      AND mask_blind_idx IS NULL
  ) THEN
    RAISE EXCEPTION '#1334: user_accounts rows have mask but NULL mask_blind_idx; run cmd/encrypt-migrate --table user_accounts first';
  END IF;
  IF EXISTS (SELECT 1 FROM user_accounts WHERE mask IS NULL AND mask_blind_idx IS NOT NULL) THEN
    RAISE EXCEPTION '#1334: user_accounts rows have NULL mask but non-NULL mask_blind_idx; data inconsistency — fix before migrating';
  END IF;
END $$;
`

// ─── Helpers ─────────────────────────────────────────────────────────────────

// seedMigTestUser inserts a minimal user row and returns the user UUID string.
// Cleanup is registered on t.
func seedMigTestUser(t *testing.T, pool *pgxpool.Pool, suffix string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|mig100-"+suffix+"-"+userID, "mig100-"+suffix+"-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) //nolint:errcheck
	})
	return userID
}

// seedMigTestLinkedItem inserts a linked_item for a user, returns the linked_item UUID string.
func seedMigTestLinkedItem(t *testing.T, tx pgx.Tx, userID, suffix string) string {
	t.Helper()
	liID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, liID, userID, "item-mig100-"+suffix+"-"+liID)
	require.NoError(t, err)
	return liID
}

// seedUserAccount inserts a user_accounts row and returns its UUID string.
// Pass empty strings for instBlindIdx / mask / maskBlindIdx to get SQL NULL.
// When maskBlindIdx is empty the row is treated as no-mask (mask stored as NULL).
func seedUserAccount(t *testing.T, tx pgx.Tx, userID, instBlindIdx, mask, maskBlindIdx string, createdAt time.Time) string {
	t.Helper()
	uaID := uuid.New().String()
	var err error
	if instBlindIdx == "" {
		// No institution blind index → fully un-backfilled row (all blind cols NULL)
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $3)
		`, uaID, userID, createdAt)
	} else if maskBlindIdx == "" {
		// No mask blind index → no-mask row (mask IS NULL, mask_blind_idx IS NULL)
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, institution_id_blind_idx, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $4, $4)
		`, uaID, userID, instBlindIdx, createdAt)
	} else {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $4, $5, $6, $6)
		`, uaID, userID, mask, instBlindIdx, maskBlindIdx, createdAt)
	}
	require.NoError(t, err)
	return uaID
}

// rollbackMigration000100 applies the real 000100 down SQL in tx:
// drops the unique index and restores the non-unique index.
func rollbackMigration000100(t *testing.T, tx pgx.Tx) {
	t.Helper()
	downSQL := readMigrationFile(t, "000100_add_user_accounts_blind_idx_unique_index.down.sql")
	_, err := tx.Exec(context.Background(), downSQL)
	require.NoError(t, err, "rollback migration 000100 failed")
}

// applyMigration000100 runs the real 000100 up SQL in tx (steps 1–7).
// Reading from the actual file guarantees the test exercises the production SQL,
// including the full JSONB rewrites and all 8 FK re-points.
func applyMigration000100(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000100_add_user_accounts_blind_idx_unique_index.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000100 failed")
}

// ─── Test suite ──────────────────────────────────────────────────────────────

func TestMigration000100_UserAccountsBlindIndex(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario A: Duplicate rows — dedup + FK re-point ────────────────────
	t.Run("ScenarioA_DedupAndFKRepoint", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("A-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		// U1: survivor (earliest created_at)
		t1 := time.Now().Add(-2 * time.Hour)
		instBlind := "blind-inst-A-" + suffix
		maskBlind := "blind-mask-A-" + suffix
		u1ID := seedUserAccount(t, tx, userID, instBlind, "1234", maskBlind, t1)

		// U2: loser (later created_at)
		t2 := time.Now().Add(-1 * time.Hour)
		u2ID := seedUserAccount(t, tx, userID, instBlind, "1234", maskBlind, t2)

		// Insert linked_item for accounts FK
		liID := seedMigTestLinkedItem(t, tx, userID, suffix)

		// Insert accounts row pointing to U2
		accID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, user_account_id)
			VALUES ($1, $2, $3, $4, 'Test Account', 'depository', $5)
		`, accID, liID, userID, "plaid-A-"+suffix, u2ID)
		require.NoError(t, err)

		// Insert transaction pointing to U2
		txRowID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name, user_account_id)
			VALUES ($1, $2, $3, $4, 'manual', 10.00, '2024-01-01', 'Test Tx A', $5)
		`, txRowID, userID, accID, liID, u2ID)
		require.NoError(t, err)

		applyMigration000100(t, tx)

		// Assert U2 is gone
		var uaCount int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id = $1", u2ID).Scan(&uaCount))
		assert.Equal(t, 0, uaCount, "loser U2 must be deleted after migration")

		// Assert U1 still exists
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id = $1", u1ID).Scan(&uaCount))
		assert.Equal(t, 1, uaCount, "survivor U1 must still exist after migration")

		// Assert accounts row re-pointed to U1
		var gotAccUAID string
		require.NoError(t, tx.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&gotAccUAID))
		assert.Equal(t, u1ID, gotAccUAID, "accounts.user_account_id must point to survivor U1")

		// Assert transactions row re-pointed to U1
		var gotTxUAID string
		require.NoError(t, tx.QueryRow(ctx, "SELECT user_account_id::text FROM transactions WHERE id = $1", txRowID).Scan(&gotTxUAID))
		assert.Equal(t, u1ID, gotTxUAID, "transactions.user_account_id must point to survivor U1")
	})

	// ─── Scenario B: JSONB references rewritten ───────────────────────────────
	t.Run("ScenarioB_JSONBRewrite", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("B-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		instBlind := "blind-inst-B-" + suffix
		maskBlind := "blind-mask-B-" + suffix

		t1 := time.Now().Add(-2 * time.Hour)
		u1ID := seedUserAccount(t, tx, userID, instBlind, "5678", maskBlind, t1)

		t2 := time.Now().Add(-1 * time.Hour)
		u2ID := seedUserAccount(t, tx, userID, instBlind, "5678", maskBlind, t2)

		// Insert linked_item for the accounts FK required by transfers
		liID := seedMigTestLinkedItem(t, tx, userID, suffix)

		// Boost B1: transfer_suggestion with from_user_account_id = loser U2, to_user_account_id = unrelated "other-id"
		boostID1 := uuid.New().String()
		boostActions1 := fmt.Sprintf(`{"transfer": {"from_user_account_id": "%s", "to_user_account_id": "other-id"}}`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO boosts (id, user_id, name, boost_type, actions, trigger_config)
			VALUES ($1, $2, $3, 'transfer_suggestion', $4::jsonb, '{}'::jsonb)
		`, boostID1, userID, "boost-B1-"+suffix, boostActions1)
		require.NoError(t, err)

		// Boost B2: transfer_suggestion with to_user_account_id = loser U2, from_user_account_id = unrelated "other-id"
		boostID2 := uuid.New().String()
		boostActions2 := fmt.Sprintf(`{"transfer": {"from_user_account_id": "other-id", "to_user_account_id": "%s"}}`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO boosts (id, user_id, name, boost_type, actions, trigger_config)
			VALUES ($1, $2, $3, 'transfer_suggestion', $4::jsonb, '{}'::jsonb)
		`, boostID2, userID, "boost-B2-"+suffix, boostActions2)
		require.NoError(t, err)

		// Boost B3: event-type trigger with trigger_config.filters.user_account_id = loser U2
		boostID3 := uuid.New().String()
		boostTrigger3 := fmt.Sprintf(`{"type": "event", "filters": {"user_account_id": "%s"}}`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO boosts (id, user_id, name, boost_type, actions, trigger_config)
			VALUES ($1, $2, $3, 'transfer_suggestion', '{}'::jsonb, $4::jsonb)
		`, boostID3, userID, "boost-B3-"+suffix, boostTrigger3)
		require.NoError(t, err)

		// Insert categorization_rule with account_id condition referencing U2
		ruleID := uuid.New().String()
		ruleConditions := fmt.Sprintf(`[{"type": "account_id", "value": "%s"}]`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO categorization_rules (id, user_id, name, conditions)
			VALUES ($1, $2, $3, $4::jsonb)
		`, ruleID, userID, "rule-B-"+suffix, ruleConditions)
		require.NoError(t, err)

		applyMigration000100(t, tx)

		// Assert boost B1: from_user_account_id rewritten to U1, to_user_account_id ("other-id") preserved
		var gotActions1 string
		require.NoError(t, tx.QueryRow(ctx, "SELECT actions::text FROM boosts WHERE id = $1", boostID1).Scan(&gotActions1))
		assert.Contains(t, gotActions1, u1ID, "boost B1 actions must reference survivor U1 in from_user_account_id")
		assert.NotContains(t, gotActions1, u2ID, "boost B1 actions must not reference loser U2")
		assert.Contains(t, gotActions1, "other-id", "boost B1 actions must preserve unrelated to_user_account_id 'other-id'")

		// Assert boost B2: to_user_account_id rewritten to U1, from_user_account_id ("other-id") preserved
		var gotActions2 string
		require.NoError(t, tx.QueryRow(ctx, "SELECT actions::text FROM boosts WHERE id = $1", boostID2).Scan(&gotActions2))
		assert.Contains(t, gotActions2, u1ID, "boost B2 actions must reference survivor U1 in to_user_account_id")
		assert.NotContains(t, gotActions2, u2ID, "boost B2 actions must not reference loser U2")
		assert.Contains(t, gotActions2, "other-id", "boost B2 actions must preserve unrelated from_user_account_id 'other-id'")

		// Assert boost B3: trigger_config.filters.user_account_id rewritten to U1
		var gotTrigger3 string
		require.NoError(t, tx.QueryRow(ctx, "SELECT trigger_config::text FROM boosts WHERE id = $1", boostID3).Scan(&gotTrigger3))
		assert.Contains(t, gotTrigger3, u1ID, "boost B3 trigger_config must reference survivor U1 in filters.user_account_id")
		assert.NotContains(t, gotTrigger3, u2ID, "boost B3 trigger_config must not reference loser U2")

		// Assert categorization_rules conditions rewritten
		var gotConditions string
		require.NoError(t, tx.QueryRow(ctx, "SELECT conditions::text FROM categorization_rules WHERE id = $1", ruleID).Scan(&gotConditions))
		assert.Contains(t, gotConditions, u1ID, "categorization_rules.conditions must reference survivor U1")
		assert.NotContains(t, gotConditions, u2ID, "categorization_rules.conditions must not reference loser U2")
	})

	// ─── Scenario C: Unique constraint enforced post-migration ────────────────
	t.Run("ScenarioC_UniqueConstraintEnforced", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("C-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		// Migration 000100 is already applied to the production schema.
		// Verify the unique index is active by attempting to insert a duplicate.
		instBlind := "blind-inst-C-" + suffix
		maskBlind := "blind-mask-C-" + suffix

		// Insert first row (should succeed)
		ua1ID := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
			VALUES ($1, $2, 'enc-inst', '9999', $3, $4)
		`, ua1ID, userID, instBlind, maskBlind)
		require.NoError(t, err)
		t.Cleanup(func() {
			pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", ua1ID) //nolint:errcheck
		})

		// Attempt to insert a duplicate (same user_id + blind indexes)
		ua2ID := uuid.New().String()
		_, err = pool.Exec(ctx, `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
			VALUES ($1, $2, 'enc-inst', '9999', $3, $4)
		`, ua2ID, userID, instBlind, maskBlind)
		require.Error(t, err, "inserting duplicate (user_id, institution_id_blind_idx, mask_blind_idx) must fail")
		errMsg := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique_user_accounts_inst_mask_blind"),
			"error must be a unique constraint violation, got: %s", err.Error(),
		)
	})

	// ─── Scenario D: Preflight check fires ───────────────────────────────────
	t.Run("ScenarioD_PreflightCheckFires", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("D-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		// Insert a user_accounts row with institution_id_blind_idx set but mask_blind_idx NULL
		// and mask non-null — this is the inconsistent state the preflight catches.
		_, err = tx.Exec(ctx, `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
			VALUES (gen_random_uuid(), $1, 'enc-inst', '1234', 'some-blind-idx', NULL)
		`, userID)
		require.NoError(t, err)

		// Run the preflight step — it must raise an exception
		_, err = tx.Exec(ctx, migration000100PreflightSQL)
		require.Error(t, err, "preflight must raise an exception for inconsistent blind-index state")
		assert.Contains(t, err.Error(), "#1334", "error message must contain the issue reference")
	})

	// ─── Scenario E: No-dupe path (idempotency) ──────────────────────────────
	t.Run("ScenarioE_NoDupePath", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("E-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		// Insert two rows with DIFFERENT institution_id_blind_idx — no duplicates
		t1 := time.Now().Add(-2 * time.Hour)
		t2 := time.Now().Add(-1 * time.Hour)
		u1ID := seedUserAccount(t, tx, userID, "blind-inst-E1-"+suffix, "1111", "blind-mask-E1-"+suffix, t1)
		u2ID := seedUserAccount(t, tx, userID, "blind-inst-E2-"+suffix, "2222", "blind-mask-E2-"+suffix, t2)

		applyMigration000100(t, tx)

		// Both rows must still exist
		var count int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id IN ($1, $2)", u1ID, u2ID).Scan(&count))
		assert.Equal(t, 2, count, "both non-duplicate rows must survive migration")
	})

	// ─── Scenario F: Rollback schema check ───────────────────────────────────
	t.Run("ScenarioF_RollbackAndReapply", func(t *testing.T) {
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		// Apply down: drop unique index, restore non-unique index
		rollbackMigration000100(t, tx)

		// Verify unique index is gone
		var uniqueExists bool
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE indexname = 'unique_user_accounts_inst_mask_blind'
			)
		`).Scan(&uniqueExists))
		assert.False(t, uniqueExists, "unique index must not exist after down migration")

		// Re-apply: full migration up (preflight passes on empty tx scope, dedup is a no-op)
		applyMigration000100(t, tx)

		// Verify unique index is back
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE indexname = 'unique_user_accounts_inst_mask_blind'
			)
		`).Scan(&uniqueExists))
		assert.True(t, uniqueExists, "unique index must exist after re-applying up migration")

		// Verify the non-unique index is gone
		var nonUniqueExists bool
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE indexname = 'idx_user_accounts_inst_mask_blind'
			)
		`).Scan(&nonUniqueExists))
		assert.False(t, nonUniqueExists, "non-unique index must not exist after up migration")
	})

	// ─── Scenario H: NULL-mask duplicate collapse ─────────────────────────────
	t.Run("ScenarioH_NullMaskDuplicateCollapse", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("H-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		instBlind := "blind-inst-H-" + suffix

		// U_H1: survivor (earliest created_at), mask = NULL, mask_blind_idx = NULL
		tH1 := time.Now().Add(-2 * time.Hour)
		uH1ID := seedUserAccount(t, tx, userID, instBlind, "", "", tH1)

		// U_H2: loser (later created_at), mask = NULL, mask_blind_idx = NULL
		tH2 := time.Now().Add(-1 * time.Hour)
		uH2ID := seedUserAccount(t, tx, userID, instBlind, "", "", tH2)

		applyMigration000100(t, tx)

		// U_H2 must be deleted (NULL mask_blind_idx is treated as equal by NULLS NOT DISTINCT)
		var uH2Count int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id = $1", uH2ID).Scan(&uH2Count))
		assert.Equal(t, 0, uH2Count, "loser U_H2 (NULL mask) must be deleted after migration")

		// U_H1 must survive
		var uH1Count int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id = $1", uH1ID).Scan(&uH1Count))
		assert.Equal(t, 1, uH1Count, "survivor U_H1 (NULL mask) must still exist after migration")
	})

	// ─── Scenario I: NULL institution_id_blind_idx triggers strict preflight ──
	// After G2, the migration aborts when ANY row has NULL institution_id_blind_idx.
	// Operators must run cmd/encrypt-migrate --table user_accounts first.
	t.Run("ScenarioI_NullBlindIdxTriggersStrictPreflight", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("I-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		// Seed a row with NULL institution_id_blind_idx (un-backfilled).
		tI1 := time.Now().Add(-2 * time.Hour)
		_ = seedUserAccount(t, tx, userID, "", "", "", tI1)

		// Running the full migration must fail — Guard 1 fires.
		upSQL := readMigrationFile(t, "000100_add_user_accounts_blind_idx_unique_index.up.sql")
		_, err = tx.Exec(ctx, upSQL)
		require.Error(t, err, "migration must raise when rows have NULL institution_id_blind_idx")
		assert.Contains(t, err.Error(), "#1334", "error must carry the issue reference")
		assert.Contains(t, err.Error(), "institution_id_blind_idx", "error must name the column")
	})

	// ─── Scenario J: categorization_rules with string + array account_id ──────
	// Regression guard for G1 (array-rewrite WHERE EXISTS abort).
	// Seeds a categorization_rule with BOTH a string-valued and an array-valued
	// account_id condition pointing at a loser — verifies both are rewritten.
	t.Run("ScenarioJ_CategorizationRulesStringAndArrayConditions", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("J-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		instBlind := "blind-inst-J-" + suffix
		maskBlind := "blind-mask-J-" + suffix

		t1 := time.Now().Add(-2 * time.Hour)
		u1ID := seedUserAccount(t, tx, userID, instBlind, "4321", maskBlind, t1)

		t2 := time.Now().Add(-1 * time.Hour)
		u2ID := seedUserAccount(t, tx, userID, instBlind, "4321", maskBlind, t2)

		// Rule with both a string-valued and an array-valued account_id condition.
		// The string condition directly references loser U2.
		// The array condition contains loser U2 plus an unrelated ID.
		ruleID := uuid.New().String()
		ruleConditions := fmt.Sprintf(
			`[{"type": "account_id", "value": "%s"}, {"type": "account_id", "value": ["%s", "unrelated-id"]}]`,
			u2ID, u2ID,
		)
		_, err = tx.Exec(ctx, `
			INSERT INTO categorization_rules (id, user_id, name, conditions)
			VALUES ($1, $2, $3, $4::jsonb)
		`, ruleID, userID, "rule-J-"+suffix, ruleConditions)
		require.NoError(t, err)

		// applyMigration000100 would panic here before the G1 fix (array SRF error).
		applyMigration000100(t, tx)

		var gotConditions string
		require.NoError(t, tx.QueryRow(ctx, "SELECT conditions::text FROM categorization_rules WHERE id = $1", ruleID).Scan(&gotConditions))

		// Both the string condition AND the array element must be rewritten to U1.
		// u1ID appears exactly twice: once from string rewrite, once from array rewrite.
		// This proves both code paths ran — not just one of them.
		assert.Equal(t, 2, strings.Count(gotConditions, u1ID),
			"survivor U1 must appear exactly twice in conditions (string + array element)")
		assert.NotContains(t, gotConditions, u2ID, "no condition must still reference loser U2")

		// "unrelated-id" must be preserved (not blanked or mapped to U1).
		assert.Contains(t, gotConditions, "unrelated-id", "unrelated array element must be preserved")
	})

	// ─── Scenario G: Customization merge ─────────────────────────────────────
	t.Run("ScenarioG_CustomizationMerge", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("G-%d", time.Now().UnixNano())

		userID := seedMigTestUser(t, pool, suffix)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		rollbackMigration000100(t, tx)

		instBlind := "blind-inst-G-" + suffix
		maskBlind := "blind-mask-G-" + suffix

		// U1: survivor (oldest), nickname=NULL, custom_color=NULL
		t1 := time.Now().Add(-3 * time.Hour)
		u1ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, nickname, custom_color, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', '0000', $3, $4, NULL, NULL, $5, $5)
		`, u1ID, userID, instBlind, maskBlind, t1)
		require.NoError(t, err)

		// U2: loser (newer), nickname='My Checking', custom_color='#FF0000'
		t2 := time.Now().Add(-2 * time.Hour)
		u2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, nickname, custom_color, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', '0000', $3, $4, 'My Checking', '#FF0000', $5, $5)
		`, u2ID, userID, instBlind, maskBlind, t2)
		require.NoError(t, err)

		applyMigration000100(t, tx)

		// U2 must be deleted
		var u2Count int
		require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_accounts WHERE id = $1", u2ID).Scan(&u2Count))
		assert.Equal(t, 0, u2Count, "loser U2 must be deleted")

		// U1 must have inherited nickname and custom_color from U2
		var gotNickname, gotColor *string
		require.NoError(t, tx.QueryRow(ctx, "SELECT nickname, custom_color FROM user_accounts WHERE id = $1", u1ID).Scan(&gotNickname, &gotColor))
		require.NotNil(t, gotNickname, "survivor U1 must have inherited nickname from loser")
		assert.Equal(t, "My Checking", *gotNickname, "survivor must inherit loser's nickname")
		require.NotNil(t, gotColor, "survivor U1 must have inherited custom_color from loser")
		assert.Equal(t, "#FF0000", *gotColor, "survivor must inherit loser's custom_color")
	})
}
