-- Reverse 000098_budget_series_id.up.sql
--
-- WARNING: This rollback is destructive.
--   - series_id is removed from budgets (data loss of any non-self-referential series groupings).
--   - savings_goal_category_detach is re-keyed back to (budget_id, savings_goal_id).
--     Tombstone rows where budget_id IS NULL cannot be restored to the old NOT NULL
--     budget_id schema and will be deleted.
--   - The FK fk_sgcd_budget (budget_id → budgets(id) ON DELETE CASCADE) is restored.
--
-- All statements are idempotent (IF EXISTS / DO $$ guards).

-- =============================================================================
-- PART 2 reversal: savings_goal_category_detach — restore (budget_id, savings_goal_id) PK
-- =============================================================================

-- 2i-rev. Drop the series_id index.
DROP INDEX IF EXISTS idx_sgcd_series_id;

-- 2g-rev. Drop the new PRIMARY KEY on (series_id, savings_goal_id).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goal_category_detach'::regclass
          AND contype = 'p'
    ) THEN
        ALTER TABLE savings_goal_category_detach DROP CONSTRAINT savings_goal_category_detach_pkey;
    END IF;
END $$;

-- 2h-rev. Restore NOT NULL on budget_id.
--         First delete any rows where budget_id is NULL (orphaned rows that cannot be
--         restored to the old schema).
DELETE FROM savings_goal_category_detach WHERE budget_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goal_category_detach'
          AND column_name = 'budget_id'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE savings_goal_category_detach ALTER COLUMN budget_id SET NOT NULL;
    END IF;
END $$;

-- 2f-rev. Deduplicate by (budget_id, savings_goal_id) before restoring old PK.
DELETE FROM savings_goal_category_detach a
USING savings_goal_category_detach b
WHERE a.ctid > b.ctid
  AND a.budget_id = b.budget_id
  AND a.savings_goal_id = b.savings_goal_id;

-- 2e-rev. Restore FK: budget_id → budgets(id) ON DELETE CASCADE.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_sgcd_budget'
          AND table_name = 'savings_goal_category_detach'
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

-- 2d-rev. Restore PRIMARY KEY on (budget_id, savings_goal_id).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goal_category_detach'::regclass
          AND contype = 'p'
    ) THEN
        ALTER TABLE savings_goal_category_detach
            ADD PRIMARY KEY (budget_id, savings_goal_id);
    END IF;
END $$;

-- 2a-rev. Drop series_id column from savings_goal_category_detach.
ALTER TABLE savings_goal_category_detach DROP COLUMN IF EXISTS series_id;

-- =============================================================================
-- PART 1 reversal: budgets — drop series_id
-- =============================================================================

-- 1d-rev. Drop series_id index.
DROP INDEX IF EXISTS idx_budgets_series_id;

-- 1a-rev. Drop series_id column from budgets.
ALTER TABLE budgets DROP COLUMN IF EXISTS series_id;
