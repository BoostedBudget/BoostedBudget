-- Issue #1300: Savings goal progress tracking modes + budget↔savings linkage.
--
-- Changes:
--   savings_goals: add progress_basis (enum), account_percentage, last_calculated_at, balance_as_of
--   savings_goal_contributions: new explicit contribution ledger (split-capable)
--   savings_goal_category_detach: tombstone table preventing auto-recreate after explicit detach
--   budget_categories: add savings_goal_id, auto_created, managed_by_goal_id; extend kind + discriminator; rebuild unique index
--
-- All statements are idempotent (IF NOT EXISTS / DO $$ guards).
-- Squawk rules followed: FKs added NOT VALID then VALIDATE in a separate step.

-- =============================================================================
-- PART 1: savings_goals — configurable progress tracking (Issue #1300, D5)
-- =============================================================================

-- 1a. Add progress_basis column (default 'manual' so existing rows keep today's behavior)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goals' AND column_name = 'progress_basis'
    ) THEN
        ALTER TABLE savings_goals
            ADD COLUMN progress_basis TEXT NOT NULL DEFAULT 'manual';
        ALTER TABLE savings_goals ALTER COLUMN progress_basis DROP DEFAULT;
    END IF;
END $$;

-- 1b. CHECK: progress_basis values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_savings_goal_progress_basis'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT valid_savings_goal_progress_basis
            CHECK (progress_basis IN ('manual', 'contributions', 'balance', 'account_percentage'));
    END IF;
END $$;

-- 1c. Add account_percentage column (required when progress_basis = 'account_percentage')
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goals' AND column_name = 'account_percentage'
    ) THEN
        ALTER TABLE savings_goals ADD COLUMN account_percentage NUMERIC;
    END IF;
END $$;

-- 1d. CHECK: account_percentage range (0 < x <= 1, or NULL)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_savings_goal_account_percentage'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT valid_savings_goal_account_percentage
            CHECK (account_percentage IS NULL OR (account_percentage > 0 AND account_percentage <= 1));
    END IF;
END $$;

-- 1e. CHECK: account_percentage discriminator
--     account_percentage must be set iff progress_basis = 'account_percentage'.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_savings_goal_percentage_discriminator'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT valid_savings_goal_percentage_discriminator
            CHECK (
                (progress_basis = 'account_percentage' AND account_percentage IS NOT NULL)
                OR
                (progress_basis != 'account_percentage' AND account_percentage IS NULL)
            ) NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goals VALIDATE CONSTRAINT valid_savings_goal_percentage_discriminator;

-- 1f. CHECK: balance/account_percentage require account_id (already NOT NULL in practice, belt+suspenders)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_savings_goal_basis_requires_account'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT valid_savings_goal_basis_requires_account
            CHECK (
                progress_basis NOT IN ('balance', 'account_percentage')
                OR account_id IS NOT NULL
            );
    END IF;
END $$;

-- 1g. Add last_calculated_at — when current_amount was last recomputed
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goals' AND column_name = 'last_calculated_at'
    ) THEN
        ALTER TABLE savings_goals ADD COLUMN last_calculated_at TIMESTAMPTZ;
    END IF;
END $$;

-- 1h. Add balance_as_of — account's last_synced_at at recompute time (freshness signal for API/MCP)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goals' AND column_name = 'balance_as_of'
    ) THEN
        ALTER TABLE savings_goals ADD COLUMN balance_as_of TIMESTAMPTZ;
    END IF;
END $$;

-- =============================================================================
-- PART 2: savings_goal_contributions — explicit, split-capable contribution ledger
-- Issue #1300 D6; Codex C1/C2/C3/C4
-- =============================================================================

CREATE TABLE IF NOT EXISTS savings_goal_contributions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Goal linkage: cascade delete so all contributions vanish when goal is deleted
    savings_goal_id UUID NOT NULL,

    -- Transaction linkage: NULL for manually entered contributions.
    -- Auto-derived rows (managed_by_reconciliation=true) use ON DELETE CASCADE
    -- via a separate FK; manual/user rows use ON DELETE SET NULL so history survives
    -- txn removal (but those rows are voided by reconciliation before counting).
    -- See FK constraints below for the split approach.
    transaction_id UUID,

    -- Amount is always the positive contribution magnitude (ABS of the transaction amount).
    -- CHECK (amount > 0) enforces sign convention — never store negative contributions.
    amount NUMERIC NOT NULL,

    -- Source records the contribution origin.
    source TEXT NOT NULL,

    -- Status: only 'confirmed' contributions count toward goal progress.
    status TEXT NOT NULL DEFAULT 'confirmed',

    -- managed_by_reconciliation: true → reconciler owns this row (may re-derive or delete).
    -- false → user-entered; reconciler must NEVER touch.   (Codex C3)
    managed_by_reconciliation BOOLEAN NOT NULL DEFAULT false,

    -- confirmed_by_user: true → a human explicitly confirmed this contribution.
    confirmed_by_user BOOLEAN NOT NULL DEFAULT false,

    occurred_on DATE NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2a. CHECK: amount must be positive
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'positive_contribution_amount'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT positive_contribution_amount
            CHECK (amount > 0);
    END IF;
END $$;

-- 2b. CHECK: source values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_contribution_source'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT valid_contribution_source
            CHECK (source IN ('manual', 'confirmed_transfer', 'boost_match'));
    END IF;
END $$;

-- 2c. CHECK: status values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_contribution_status'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT valid_contribution_status
            CHECK (status IN ('proposed', 'confirmed'));
    END IF;
END $$;

-- 2d. FK: savings_goal_id → savings_goals(id) ON DELETE CASCADE
-- #1569 T8 (r17) / T2 (r18): the guard is qualified by table_name, table_schema AND
-- constraint_type. information_schema.table_constraints lists every constraint type and is keyed
-- by (constraint_schema, constraint_name), so all three predicates are load-bearing:
--   * without table_name, a same-named constraint on another table answers the guard;
--   * without table_schema, a same-named constraint on a same-named table in another visible
--     schema answers it while the unqualified ALTER TABLE below targets the search_path table --
--     reproduced in r18: the ADD is skipped and the VALIDATE below then errors (loud, not silent);
--   * without constraint_type, a same-named CHECK on THIS table answers it -- and because
--     VALIDATE CONSTRAINT accepts a CHECK, the VALIDATE below passes and the migration SUCCEEDS
--     with the foreign key silently absent. Reproduced in r18 against a scratch copy of the
--     migrated schema.
-- Constraint names are unique per table, so with constraint_type in place a same-named non-FK
-- decoy on this table makes the ADD fail loudly instead of being skipped. Pinned by
-- TestMigrationFKGuards_SameNamedNonForeignKeyMustNotSuppressTheCascade and
-- TestMigrationFKGuards_SameNamedConstraintInAnotherSchemaMustNotSuppressTheCascade.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_contributions_savings_goal'
          AND table_name = 'savings_goal_contributions'
          AND table_schema = current_schema()
          AND constraint_type = 'FOREIGN KEY'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT fk_contributions_savings_goal
            FOREIGN KEY (savings_goal_id)
            REFERENCES savings_goals(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_contributions VALIDATE CONSTRAINT fk_contributions_savings_goal;

-- 2e. FK: transaction_id → transactions(id).
--     Auto-derived rows (managed_by_reconciliation=true) should be cleaned up by
--     the reconciler BEFORE the transaction is deleted, so ON DELETE SET NULL is
--     a safety net (the row is marked voided by reconciliation, not auto-counted).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_contributions_transaction'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT fk_contributions_transaction
            FOREIGN KEY (transaction_id)
            REFERENCES transactions(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_contributions VALIDATE CONSTRAINT fk_contributions_transaction;

-- 2f. Partial UNIQUE index: (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL.
--     Supports splitting one transaction across multiple goals (distinct savings_goal_ids).
--     Prevents double-claiming the same transaction for the same goal.
--     Manual rows (transaction_id NULL) are exempt.   (Codex C2)
CREATE UNIQUE INDEX IF NOT EXISTS unique_contribution_per_goal_per_transaction
    ON savings_goal_contributions (transaction_id, savings_goal_id)
    WHERE transaction_id IS NOT NULL;

-- 2g. Performance indexes
CREATE INDEX IF NOT EXISTS idx_contributions_savings_goal_id
    ON savings_goal_contributions (savings_goal_id);

CREATE INDEX IF NOT EXISTS idx_contributions_savings_goal_occurred_on
    ON savings_goal_contributions (savings_goal_id, occurred_on);

CREATE INDEX IF NOT EXISTS idx_contributions_savings_goal_status
    ON savings_goal_contributions (savings_goal_id, status);

-- 2h. Updated_at trigger
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_savings_goal_contributions_updated_at'
    ) THEN
        CREATE TRIGGER update_savings_goal_contributions_updated_at
            BEFORE UPDATE ON savings_goal_contributions
            FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 3: savings_goal_category_detach — tombstone table (Codex C6)
-- Records user-intent to detach a savings goal from a specific budget's
-- auto-created category. Auto-create and carry-forward skip (budget_id, goal_id)
-- pairs present here. Cleared when the user manually re-links the goal.
-- =============================================================================

CREATE TABLE IF NOT EXISTS savings_goal_category_detach (
    budget_id       UUID NOT NULL,
    savings_goal_id UUID NOT NULL,
    detached_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (budget_id, savings_goal_id)
);

-- FK: budget_id
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_sgcd_budget'
    ) THEN
        ALTER TABLE savings_goal_category_detach
            ADD CONSTRAINT fk_sgcd_budget
            FOREIGN KEY (budget_id)
            REFERENCES budgets(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_category_detach VALIDATE CONSTRAINT fk_sgcd_budget;

-- FK: savings_goal_id
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_sgcd_savings_goal'
    ) THEN
        ALTER TABLE savings_goal_category_detach
            ADD CONSTRAINT fk_sgcd_savings_goal
            FOREIGN KEY (savings_goal_id)
            REFERENCES savings_goals(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;
ALTER TABLE savings_goal_category_detach VALIDATE CONSTRAINT fk_sgcd_savings_goal;

-- =============================================================================
-- PART 4: budget_categories — add savings_goal_id, auto_created, managed_by_goal_id
-- extend kind CHECK; update discriminator CHECK; rebuild unique index.
-- =============================================================================

-- 4a. Add savings_goal_id column
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'savings_goal_id'
    ) THEN
        ALTER TABLE budget_categories ADD COLUMN savings_goal_id UUID;
    END IF;
END $$;

-- 4b. Add auto_created flag
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'auto_created'
    ) THEN
        ALTER TABLE budget_categories
            ADD COLUMN auto_created BOOLEAN NOT NULL DEFAULT false;
    END IF;
END $$;

-- 4c. Add managed_by_goal_id column
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'managed_by_goal_id'
    ) THEN
        ALTER TABLE budget_categories ADD COLUMN managed_by_goal_id UUID;
    END IF;
END $$;

-- 4d. FK: savings_goal_id → savings_goals(id) ON DELETE CASCADE
-- #1569 T8 (r17) / T2 (r18): the guard is qualified by table_name, table_schema AND
-- constraint_type. information_schema.table_constraints lists every constraint type and is keyed
-- by (constraint_schema, constraint_name), so all three predicates are load-bearing:
--   * without table_name, a same-named constraint on another table answers the guard;
--   * without table_schema, a same-named constraint on a same-named table in another visible
--     schema answers it while the unqualified ALTER TABLE below targets the search_path table --
--     reproduced in r18: the ADD is skipped and the VALIDATE below then errors (loud, not silent);
--   * without constraint_type, a same-named CHECK on THIS table answers it -- and because
--     VALIDATE CONSTRAINT accepts a CHECK, the VALIDATE below passes and the migration SUCCEEDS
--     with the foreign key silently absent. Reproduced in r18 against a scratch copy of the
--     migrated schema.
-- Constraint names are unique per table, so with constraint_type in place a same-named non-FK
-- decoy on this table makes the ADD fail loudly instead of being skipped. Pinned by
-- TestMigrationFKGuards_SameNamedNonForeignKeyMustNotSuppressTheCascade and
-- TestMigrationFKGuards_SameNamedConstraintInAnotherSchemaMustNotSuppressTheCascade.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_budget_categories_savings_goal'
          AND table_name = 'budget_categories'
          AND table_schema = current_schema()
          AND constraint_type = 'FOREIGN KEY'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT fk_budget_categories_savings_goal
            FOREIGN KEY (savings_goal_id)
            REFERENCES savings_goals(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;
ALTER TABLE budget_categories VALIDATE CONSTRAINT fk_budget_categories_savings_goal;

-- 4e. FK: managed_by_goal_id → savings_goals(id) ON DELETE SET NULL
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_budget_categories_managed_by_goal'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT fk_budget_categories_managed_by_goal
            FOREIGN KEY (managed_by_goal_id)
            REFERENCES savings_goals(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;
ALTER TABLE budget_categories VALIDATE CONSTRAINT fk_budget_categories_managed_by_goal;

-- 4f. Extend valid_budget_category_kind to include 'savings'.
--     Drop the old CHECK (IN ('pfc','custom')) and recreate with 'savings'.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_kind'
    ) THEN
        ALTER TABLE budget_categories DROP CONSTRAINT valid_budget_category_kind;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_kind'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT valid_budget_category_kind
            CHECK (category_kind IN ('pfc', 'custom', 'savings'));
    END IF;
END $$;

-- 4g. Update the multi-branch discriminator CHECK to include the savings branch
--     and tighten pfc/custom branches to require savings_goal_id IS NULL.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_discriminator'
    ) THEN
        ALTER TABLE budget_categories DROP CONSTRAINT valid_budget_category_discriminator;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_discriminator'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT valid_budget_category_discriminator
            CHECK (
                (category_kind = 'pfc'
                    AND pfc_primary IS NOT NULL
                    AND custom_category_id IS NULL
                    AND savings_goal_id IS NULL)
                OR
                (category_kind = 'custom'
                    AND custom_category_id IS NOT NULL
                    AND pfc_primary IS NULL
                    AND pfc_detailed IS NULL
                    AND savings_goal_id IS NULL)
                OR
                (category_kind = 'savings'
                    AND savings_goal_id IS NOT NULL
                    AND pfc_primary IS NULL
                    AND pfc_detailed IS NULL
                    AND custom_category_id IS NULL)
            ) NOT VALID;
    END IF;
END $$;
ALTER TABLE budget_categories VALIDATE CONSTRAINT valid_budget_category_discriminator;

-- 4h. Rebuild unique_budget_category_discriminated index to include savings_goal_id.
--     Preflight: remove any existing duplicates including savings_goal_id column.
--     (For pfc/custom rows, savings_goal_id is NULL; for savings rows it's the goal UUID.)
DELETE FROM budget_categories a
USING budget_categories b
WHERE a.ctid > b.ctid
  AND a.budget_id = b.budget_id
  AND a.category_kind = b.category_kind
  AND a.pfc_primary IS NOT DISTINCT FROM b.pfc_primary
  AND a.pfc_detailed IS NOT DISTINCT FROM b.pfc_detailed
  AND a.custom_category_id IS NOT DISTINCT FROM b.custom_category_id
  AND a.savings_goal_id IS NOT DISTINCT FROM b.savings_goal_id;

-- Drop the old 5-column index.
DROP INDEX IF EXISTS unique_budget_category_discriminated;

-- Recreate as 6-column UNIQUE INDEX with NULLS NOT DISTINCT.
-- Allows: same goal in different budgets ✓, distinct goals in same budget ✓
-- Forbids: same goal twice in one budget ✗, same pfc/custom discriminator twice ✗
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
    ON budget_categories (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id, savings_goal_id)
    NULLS NOT DISTINCT;

-- 4i. Index for efficient lookup of savings categories by goal
CREATE INDEX IF NOT EXISTS idx_budget_categories_budget_savings
    ON budget_categories (budget_id, savings_goal_id)
    WHERE category_kind = 'savings';
