-- Rollback for 000158: reverse ONLY what the up migration added.
--
-- READ THIS BEFORE ROLLING BACK.
--
--   1. Dropping deleted_at makes every tombstoned savings goal, boost, budget and budget category
--      VISIBLE AGAIN, all at once, with no way to tell them apart from live rows -- the
--      classification lives only in these columns. A user who deleted a goal in the UI would find
--      it back, its contributions counting toward totals again.
--   2. Under the post-000158 BINARY this rollback breaks the four delete paths outright: the
--      repositories issue `UPDATE ... SET deleted_at = ...`, which fails on a missing column, and
--      the reads filter on it. Roll the application back first, or not at all.
--   3. Restoring the unconditional UNIQUE constraints can FAIL. If two rows share
--      (user_id, name) -- which the partial index permitted, one live and one tombstoned -- the
--      ALTER TABLE ... ADD CONSTRAINT below aborts. That is deliberate: silently dropping one of
--      them would destroy user data on a rollback. Resolve the duplicates by hand first:
--        SELECT user_id, name, count(*) FROM savings_goals GROUP BY 1,2 HAVING count(*) > 1;
--      and the same for boosts and budgets.
--
-- If a rollback is genuinely required, dump
--   SELECT id, deleted_at, deleted_reason, deleted_by FROM <table> WHERE deleted_at IS NOT NULL
-- for all four tables first, so the classification can be restored on the way back up.
--
-- Idempotent: IF EXISTS on every statement, pg_constraint guards on the constraint restores.

DROP INDEX IF EXISTS idx_savings_goals_tombstoned;
DROP INDEX IF EXISTS idx_boosts_tombstoned;
DROP INDEX IF EXISTS idx_budgets_tombstoned;
DROP INDEX IF EXISTS idx_budget_categories_tombstoned;

-- Restore the unconditional uniqueness. The partial unique indexes are dropped first so their
-- names are free for the constraints' implicit indexes to reuse.
DROP INDEX IF EXISTS unique_savings_goal_name_per_user;
DROP INDEX IF EXISTS unique_boost_name_per_user;
DROP INDEX IF EXISTS unique_budget_name_per_user;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'unique_savings_goal_name_per_user') THEN
        ALTER TABLE savings_goals ADD CONSTRAINT unique_savings_goal_name_per_user UNIQUE (user_id, name);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'unique_boost_name_per_user') THEN
        ALTER TABLE boosts ADD CONSTRAINT unique_boost_name_per_user UNIQUE (user_id, name);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'unique_budget_name_per_user') THEN
        ALTER TABLE budgets ADD CONSTRAINT unique_budget_name_per_user UNIQUE (user_id, name);
    END IF;
END $$;

DROP INDEX IF EXISTS unique_budget_category_discriminated;
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
    ON budget_categories (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id, savings_goal_id)
    NULLS NOT DISTINCT;

ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS savings_goals_deleted_reason_valid;
ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS savings_goals_tombstone_consistency;
ALTER TABLE boosts DROP CONSTRAINT IF EXISTS boosts_deleted_reason_valid;
ALTER TABLE boosts DROP CONSTRAINT IF EXISTS boosts_tombstone_consistency;
ALTER TABLE budgets DROP CONSTRAINT IF EXISTS budgets_deleted_reason_valid;
ALTER TABLE budgets DROP CONSTRAINT IF EXISTS budgets_tombstone_consistency;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS budget_categories_deleted_reason_valid;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS budget_categories_tombstone_consistency;

ALTER TABLE savings_goals DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE boosts DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE boosts DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE boosts DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE budgets DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE budgets DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE budgets DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS deleted_at;
