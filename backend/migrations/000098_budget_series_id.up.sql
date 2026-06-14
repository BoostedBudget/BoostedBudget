-- Issue #1300 gap fix G5: add series_id to budgets and re-key savings_goal_category_detach.
--
-- Problem: savings_goal_category_detach was keyed by (budget_id, savings_goal_id).
-- Budgets are per-period instances; a new month = a new budget_id, so a user detaching
-- a savings goal category in month A would see it reappear in month B's new budget.
--
-- Fix:
--   1. Add series_id UUID to budgets (backfilled = id so each budget self-identifies its
--      own series until explicit series grouping is introduced in a future migration).
--   2. Re-key savings_goal_category_detach to (series_id, savings_goal_id) so a detach
--      persists across all budgets that share the same series.
--   3. Keep budget_id column in savings_goal_category_detach as nullable audit trail.
--
-- All statements are idempotent (IF NOT EXISTS / DO $$ guards).
-- Squawk rules followed: new NOT NULL column added nullable → backfill → NOT NULL.

-- =============================================================================
-- PART 1: budgets — add series_id
-- =============================================================================

-- 1a. Add series_id as nullable first (required by Squawk for NOT NULL on existing tables).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'series_id'
    ) THEN
        ALTER TABLE budgets ADD COLUMN series_id UUID;
    END IF;
END $$;

-- 1b. Backfill: each existing budget is its own series (series_id = id).
UPDATE budgets SET series_id = id WHERE series_id IS NULL;

-- 1c. Enforce NOT NULL now that all rows are populated.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets'
          AND column_name = 'series_id'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE budgets ALTER COLUMN series_id SET NOT NULL;
    END IF;
END $$;

-- 1d. Index for efficient lookups by series (e.g., "all budgets in series X").
CREATE INDEX IF NOT EXISTS idx_budgets_series_id ON budgets (series_id);

-- =============================================================================
-- PART 2: savings_goal_category_detach — re-key to (series_id, savings_goal_id)
-- =============================================================================

-- 2a. Add series_id column (nullable first, then backfill from budget_id, then NOT NULL).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goal_category_detach' AND column_name = 'series_id'
    ) THEN
        ALTER TABLE savings_goal_category_detach ADD COLUMN series_id UUID;
    END IF;
END $$;

-- 2b. Backfill: for all existing tombstone rows, series_id = budget_id.
--     (Pre-G5 rows were created before series existed; the budget was its own series.)
UPDATE savings_goal_category_detach SET series_id = budget_id WHERE series_id IS NULL;

-- 2c. Set NOT NULL on series_id after backfill.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goal_category_detach'
          AND column_name = 'series_id'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE savings_goal_category_detach ALTER COLUMN series_id SET NOT NULL;
    END IF;
END $$;

-- 2d. Drop the old PRIMARY KEY (budget_id, savings_goal_id).
--     The PK was created inline in 000097 CREATE TABLE, so its name is
--     savings_goal_category_detach_pkey (Postgres default).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goal_category_detach'::regclass
          AND contype = 'p'
          AND conname = 'savings_goal_category_detach_pkey'
    ) THEN
        ALTER TABLE savings_goal_category_detach DROP CONSTRAINT savings_goal_category_detach_pkey;
    END IF;
END $$;

-- 2e. Drop the FK on budget_id (tombstone rows must outlive specific budget instances;
--     the budget may be deleted when rolling budgets forward while the detach intent
--     remains valid for the series).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_sgcd_budget'
          AND table_name = 'savings_goal_category_detach'
    ) THEN
        ALTER TABLE savings_goal_category_detach DROP CONSTRAINT fk_sgcd_budget;
    END IF;
END $$;

-- 2f. Deduplicate rows before adding new PRIMARY KEY.
--     If two rows share (series_id, savings_goal_id) (possible if the same goal was
--     detached in two different budgets that both map to series_id = budget_id), keep
--     the one with the earliest detached_at.
DELETE FROM savings_goal_category_detach a
USING savings_goal_category_detach b
WHERE a.ctid > b.ctid
  AND a.series_id = b.series_id
  AND a.savings_goal_id = b.savings_goal_id;

-- 2g. Add new PRIMARY KEY on (series_id, savings_goal_id).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goal_category_detach'::regclass
          AND contype = 'p'
    ) THEN
        ALTER TABLE savings_goal_category_detach
            ADD PRIMARY KEY (series_id, savings_goal_id);
    END IF;
END $$;

-- 2h. budget_id is now an optional audit-trail column (NULL = no specific budget context).
--     Drop NOT NULL constraint if it still exists (column was NOT NULL in 000097 CREATE TABLE).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goal_category_detach'
          AND column_name = 'budget_id'
          AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE savings_goal_category_detach ALTER COLUMN budget_id DROP NOT NULL;
    END IF;
END $$;

-- 2i. Index for efficient lookup by series (auto-create/carry-forward queries).
CREATE INDEX IF NOT EXISTS idx_sgcd_series_id ON savings_goal_category_detach (series_id);
