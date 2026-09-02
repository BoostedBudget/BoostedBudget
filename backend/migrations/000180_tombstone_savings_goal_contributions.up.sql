-- Issue #1569 Round 12 Task 2 — stop a Plaid `removed` event from HARD-DELETING savings history.
--
-- transaction_sync_service.go's processRemovedTransactions calls DeleteManagedForTransaction for
-- every entry in Plaid's `removed` array, which today runs a real `DELETE FROM
-- savings_goal_contributions`. That is SIDE-EFFECT destruction (the ADR's forbidden class): the
-- provider withdrawing a transaction is not the user asking for a contribution to be destroyed.
-- "the provider is not the user" applies here exactly as it does to accounts and transactions,
-- which this table is the last of the three to catch up to.
--
-- This migration adds the tombstone columns; the Go side (savings_contribution_repository.go)
-- converts the hard delete into an UPDATE in the SAME COMMIT, so the column never exists without
-- its filters -- a tombstone column with no read discipline behind it is a data leak in waiting.
--
-- WHY deleted_reason HAS ONE VALUE, NOT TWO. savings_goals/boosts/budgets/budget_categories
-- (migration 000158) classify a USER action (user_deleted / parent_deleted); transactions
-- (migration 000156) classify a PROVIDER action with two reasons because pending-vs-posted changes
-- what is shown. This table's ONLY hard-delete-turned-tombstone path is the provider withdrawing
-- the transaction a reconciler-owned contribution was derived from, so one value names it
-- precisely: 'provider_removed_txn'. DeleteContribution (the user-facing endpoint) is deliberately
-- untouched this round -- it stays a real, narrow, explicit hard delete -- so no 'user_deleted'
-- value is needed here yet.
--
-- Idempotent: IF NOT EXISTS on the columns, pg_constraint guards on every CHECK, a definition
-- check before touching the pre-existing unique index.

-- =============================================================================
-- Step 1: Tombstone columns (idempotent).
-- =============================================================================

ALTER TABLE savings_goal_contributions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE savings_goal_contributions ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);

-- =============================================================================
-- Step 2: The two columns move together, and the reason vocabulary is closed.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'savings_goal_contributions_tombstone_consistency'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT savings_goal_contributions_tombstone_consistency
            CHECK (
                (deleted_at IS NULL AND deleted_reason IS NULL)
                OR (deleted_at IS NOT NULL AND deleted_reason IS NOT NULL)
            ) NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_contributions VALIDATE CONSTRAINT savings_goal_contributions_tombstone_consistency;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'savings_goal_contributions_deleted_reason_valid'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT savings_goal_contributions_deleted_reason_valid
            CHECK (
                deleted_reason IS NULL
                OR deleted_reason IN ('provider_removed_txn')
            ) NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_contributions VALIDATE CONSTRAINT savings_goal_contributions_deleted_reason_valid;

-- =============================================================================
-- Step 3: Re-scope the (transaction_id, savings_goal_id) uniqueness to LIVE rows only.
--
-- Without this, a tombstoned row keeps occupying its slot in
-- unique_contribution_per_goal_per_transaction forever: the same (transaction, goal) pair could
-- never be re-derived (a later boost match, a corrected re-sync) because the INSERT ... ON
-- CONFLICT arbiter would have to resolve against a row the tombstone made permanently invisible.
-- The tombstoned row itself is untouched history; a fresh row for the same key is a new, current
-- fact, exactly the same relationship a tombstoned savings_goals row has to a brand new goal of
-- the same name (migration 000158, unique_savings_goal_name_per_user).
--
-- Guarded by a definition check rather than an unconditional DROP + CREATE, so a second run of
-- this migration against an already-migrated database is a no-op instead of dropping and
-- rebuilding the index every time (#1569 round 12 task 1 made the idempotency gate genuinely
-- re-execute every .up.sql, so this matters).
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'unique_contribution_per_goal_per_transaction'
          AND indexdef NOT LIKE '%deleted_at IS NULL%'
    ) THEN
        DROP INDEX unique_contribution_per_goal_per_transaction;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS unique_contribution_per_goal_per_transaction
    ON savings_goal_contributions (transaction_id, savings_goal_id)
    WHERE transaction_id IS NOT NULL AND deleted_at IS NULL;

-- =============================================================================
-- Step 4: Index the tombstones only -- live reads filter `deleted_at IS NULL`, which matches
-- nearly every row and is already served by the existing (savings_goal_id, ...) indexes.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_savings_goal_contributions_tombstoned
    ON savings_goal_contributions (savings_goal_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;

-- =============================================================================
-- Step 5: GDPR -- a tombstoned row is still a row. savings_goal_contributions reaches users(id) by
-- fk_contributions_savings_goal (savings_goal_id -> savings_goals, ON DELETE CASCADE, migration
-- 000097) and savings_goals.user_id -> users ON DELETE CASCADE, both untouched here, so
-- `DELETE FROM users` still destroys these rows regardless of deleted_at.
-- =============================================================================

COMMENT ON COLUMN savings_goal_contributions.deleted_at IS
$COMMENT$When this contribution was tombstoned. NULL for every live row; every read default-excludes
rows where this is NOT NULL. Set instead of DELETE so a Plaid `removed` event on the underlying
transaction cannot destroy a record of the user's money moving. There is no user-initiated restore
on this branch -- a tombstoned row is retained and permanently invisible in the product. GDPR
erasure (DELETE FROM users) still reaches it via the ON DELETE CASCADE chain through savings_goals.
Issue #1569.$COMMENT$;

COMMENT ON COLUMN savings_goal_contributions.deleted_reason IS
$COMMENT$Why this contribution was tombstoned. Exactly one value today: 'provider_removed_txn' --
DeleteManagedForTransaction ran because Plaid reported the underlying transaction as `removed`.
Distinct from the savings_goals/boosts/budgets/budget_categories vocabulary (migration 000158),
which classifies a USER action; this one classifies a PROVIDER action, the same distinction
transactions.deleted_reason draws for its own two values. Issue #1569.$COMMENT$;
