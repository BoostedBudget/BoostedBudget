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

// ─── Down/Up SQL snippets ────────────────────────────────────────────────────

// migration000100Down undoes migration 000100: drops the unique index and
// restores the non-unique index. This is safe to run inside a Postgres
// transaction (DDL is transactional in Postgres).
const migration000100Down = `
DROP INDEX IF EXISTS unique_user_accounts_inst_mask_blind;
CREATE INDEX IF NOT EXISTS idx_user_accounts_inst_mask_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx);
`

// migration000100PreflightSQL is the Step 1 DO block from the up migration.
const migration000100PreflightSQL = `
DO $$ BEGIN
  IF EXISTS (
    SELECT 1 FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
      AND mask IS NOT NULL
      AND mask_blind_idx IS NULL
  ) THEN
    RAISE EXCEPTION '#1334: user_accounts rows have mask but NULL mask_blind_idx; run cmd/encrypt-migrate --table user_accounts first';
  END IF;
END $$;
`

// migration000100DeduplicateSQL contains Steps 2–6 of the up migration
// (merge customizations, rewrite JSONB, re-point FK, delete losers).
const migration000100DeduplicateSQL = `
WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
agg AS (
    SELECT
        d.survivor_id,
        (array_remove(array_agg(ua.nickname          ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS nickname,
        (array_remove(array_agg(ua.custom_color      ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS custom_color,
        (array_remove(array_agg(ua.custom_icon       ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS custom_icon,
        (array_remove(array_agg(ua.custom_emoji      ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS custom_emoji,
        (array_remove(array_agg(ua.account_name_hint ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS account_name_hint,
        (array_remove(array_agg(ua.primary_type      ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS primary_type,
        (array_remove(array_agg(ua.primary_subtype   ORDER BY ua.updated_at DESC NULLS LAST), NULL))[1] AS primary_subtype,
        bool_and(ua.is_hidden) AS all_hidden
    FROM dupes d
    JOIN user_accounts ua ON ua.id = d.id
    GROUP BY d.survivor_id
    HAVING count(*) > 1
)
UPDATE user_accounts ua
SET
    nickname          = COALESCE(ua.nickname,          agg.nickname),
    custom_color      = COALESCE(ua.custom_color,      agg.custom_color),
    custom_icon       = COALESCE(ua.custom_icon,       agg.custom_icon),
    custom_emoji      = COALESCE(ua.custom_emoji,      agg.custom_emoji),
    account_name_hint = COALESCE(ua.account_name_hint, agg.account_name_hint),
    primary_type      = COALESCE(ua.primary_type,      agg.primary_type),
    primary_subtype   = COALESCE(ua.primary_subtype,   agg.primary_subtype),
    is_hidden         = agg.all_hidden,
    updated_at        = now()
FROM agg
WHERE ua.id = agg.survivor_id;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,from_user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'from_user_account_id' = l.loser_id::text;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,to_user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'to_user_account_id' = l.loser_id::text;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE boosts b
SET trigger_config = jsonb_set(b.trigger_config, '{filters,user_account_id}', to_jsonb(l.survivor_id::text))
FROM losers l
WHERE b.trigger_config->>'type' = 'event'
  AND b.trigger_config->'filters'->>'user_account_id' = l.loser_id::text;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE categorization_rules cr
SET conditions = (
    SELECT COALESCE(jsonb_agg(
        CASE
            WHEN (cond->>'type') = 'account_id'
                AND jsonb_typeof(cond->'value') = 'string'
            THEN jsonb_set(
                cond, '{value}',
                COALESCE(
                    (SELECT to_jsonb(l2.survivor_id::text)
                     FROM losers l2
                     WHERE l2.loser_id::text = cond->>'value'
                     LIMIT 1),
                    cond->'value'
                )
            )
            ELSE cond
        END
    ), '[]'::jsonb)
    FROM jsonb_array_elements(cr.conditions) AS cond
)
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(cr.conditions) AS c,
         losers l3
    WHERE c->>'type' = 'account_id'
      AND jsonb_typeof(c->'value') = 'string'
      AND l3.loser_id::text = c->>'value'
);

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE accounts a
SET user_account_id = l.survivor_id
FROM losers l
WHERE a.user_account_id = l.loser_id;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
UPDATE transactions t
SET user_account_id = l.survivor_id
FROM losers l
WHERE t.user_account_id = l.loser_id;

WITH dupes AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY user_id, institution_id_blind_idx, mask_blind_idx
               ORDER BY created_at ASC, id ASC
               ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING
           ) AS survivor_id
    FROM user_accounts
    WHERE institution_id_blind_idx IS NOT NULL
),
losers AS (
    SELECT id AS loser_id, survivor_id FROM dupes WHERE id <> survivor_id
)
DELETE FROM user_accounts ua USING losers l WHERE ua.id = l.loser_id;
`

// migration000100IndexSQL is Step 7: drop old index, create unique index.
const migration000100IndexSQL = `
DROP INDEX IF EXISTS idx_user_accounts_inst_mask_blind;
CREATE UNIQUE INDEX IF NOT EXISTS unique_user_accounts_inst_mask_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx)
    NULLS NOT DISTINCT
    WHERE institution_id_blind_idx IS NOT NULL;
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
// blindIdx and maskBlindIdx may be empty (NULL).
func seedUserAccount(t *testing.T, tx pgx.Tx, userID, instBlindIdx, mask, maskBlindIdx string, createdAt time.Time) string {
	t.Helper()
	uaID := uuid.New().String()
	var err error
	if instBlindIdx == "" {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $3)
		`, uaID, userID, createdAt)
	} else if maskBlindIdx == "" {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $4, $5, $5)
		`, uaID, userID, mask, instBlindIdx, createdAt)
	} else {
		_, err = tx.Exec(context.Background(), `
			INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, created_at, updated_at)
			VALUES ($1, $2, 'enc-inst', $3, $4, $5, $6, $6)
		`, uaID, userID, mask, instBlindIdx, maskBlindIdx, createdAt)
	}
	require.NoError(t, err)
	return uaID
}

// rollbackMigration000100 drops the unique index and restores the non-unique index in tx.
func rollbackMigration000100(t *testing.T, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(context.Background(), migration000100Down)
	require.NoError(t, err, "rollback migration 000100 failed")
}

// applyMigration000100 runs the full migration 000100 up SQL (steps 1–7) in tx.
func applyMigration000100(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	_, err := tx.Exec(ctx, migration000100PreflightSQL)
	require.NoError(t, err, "migration 000100 preflight step failed")
	_, err = tx.Exec(ctx, migration000100DeduplicateSQL)
	require.NoError(t, err, "migration 000100 dedup+repoint step failed")
	_, err = tx.Exec(ctx, migration000100IndexSQL)
	require.NoError(t, err, "migration 000100 index step failed")
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

		// Placeholder account for accounts FK (required by boosts user_id FK check in some DBs)
		// Actually boosts just needs user_id FK, not accounts. Let's insert directly.

		// Insert boost with transfer suggestion referencing U2.from_user_account_id
		boostID := uuid.New().String()
		boostActions := fmt.Sprintf(`{"transfer": {"from_user_account_id": "%s", "to_user_account_id": "other-id"}}`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO boosts (id, user_id, name, boost_type, actions, trigger_config)
			VALUES ($1, $2, $3, 'transfer_suggestion', $4::jsonb, '{}'::jsonb)
		`, boostID, userID, "boost-B-"+suffix, boostActions)
		require.NoError(t, err)

		// Insert categorization_rule with account_id condition referencing U2
		ruleID := uuid.New().String()
		ruleConditions := fmt.Sprintf(`[{"type": "account_id", "value": "%s"}]`, u2ID)
		_, err = tx.Exec(ctx, `
			INSERT INTO categorization_rules (id, user_id, name, conditions)
			VALUES ($1, $2, $3, $4::jsonb)
		`, ruleID, userID, "rule-B-"+suffix, ruleConditions)
		require.NoError(t, err)

		_ = liID // linked_item seeded but not needed for boost/rule

		applyMigration000100(t, tx)

		// Assert boosts JSONB rewritten: from_user_account_id → U1
		var gotActions string
		require.NoError(t, tx.QueryRow(ctx, "SELECT actions::text FROM boosts WHERE id = $1", boostID).Scan(&gotActions))
		assert.Contains(t, gotActions, u1ID, "boosts.actions must reference survivor U1")
		assert.NotContains(t, gotActions, u2ID, "boosts.actions must not reference loser U2")

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

		// Re-apply: steps 1–7 (no data changes needed since no dupes in this tx scope)
		_, err = tx.Exec(ctx, migration000100PreflightSQL)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, migration000100IndexSQL)
		require.NoError(t, err)

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
