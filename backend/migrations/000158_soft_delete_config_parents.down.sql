-- Rollback for 000158: reverse ONLY what the up migration added.
--
-- ⚠⚠ #1569 round 12 task 4 (fix round 1): this file is a DIFFERENT hazard class from the
-- REFUSE-while-rows-exist guards added elsewhere this round (000016/000017/000020/000027/
-- 000051/000096/000097/000157/000160/000163/000164/000169, plus the earlier
-- 000150/000151/000180). Those guards stop a DROP/DELETE from destroying rows that exist
-- nowhere else. This file destroys nothing -- every tombstoned row is still physically present
-- afterward. What it does is RESURRECT: dropping deleted_at makes a tombstoned row
-- indistinguishable from a live one, so a user who deleted a goal finds it back, counting toward
-- totals again, with no data actually missing. Destruction and resurrection want the same
-- instinct ("don't run this on real data") but a different fix -- a row-count refusal guard is
-- the wrong tool for "this data still exists, but the meaning of a column it depends on is about
-- to disappear." That is why this file was deliberately left uninstrumented in
-- round 12 task 4 rather than given the same DO-block treatment: the three numbered warnings
-- below are the right shape of fix for THIS hazard, not a gap. If a future round wants this
-- enforced rather than documented, it needs its own mechanism (e.g. detecting whether any row
-- has deleted_at IS NOT NULL and refusing on THAT basis, which is closer to 000151's shape than
-- to a plain row-count check) -- do not reach for the destruction-guard pattern unmodified.
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
-- HOW TO ACTUALLY PROCEED, if a rollback is genuinely required. ⚠ This used to say only "dump the
-- classification for all four tables first", which is step 1 of 3 and, on its own, an instruction
-- that CANNOT WORK: the DO block below refuses while a single tombstone exists in ANY of the four,
-- so a dump leaves the rollback exactly as blocked as it was. The steps that do work:
--
--   1. Dump the classification, for EVERY one of savings_goals, boosts, budgets and
--      budget_categories -- the refusal counts all four and dumping only the table you expected
--      leaves the other three blocking:
--        SELECT id, deleted_at, deleted_reason, deleted_by FROM <table> WHERE deleted_at IS NOT NULL
--   2. Empty the predicate the guard refuses on. Two ways, both lossy, and the choice is a human
--      decision per row rather than a command to run reflexively:
--        (a) UPDATE <table> SET deleted_at = NULL, deleted_reason = NULL, deleted_by = NULL
--            WHERE deleted_at IS NOT NULL;
--            keeps every row and RESURRECTS it -- warning 1. It also makes warning 3 live, and
--            EARLIER than warning 3 says: the LIVE-only partial unique indexes
--            (unique_savings_goal_name_per_user / _boost_ / _budget_, all WHERE deleted_at IS
--            NULL, plus unique_budget_category_discriminated) are enforced by this very UPDATE, so
--            it aborts on the first resurrected row whose (user_id, name) already exists live --
--            before the unconditional UNIQUE restored further down is ever reached. Rename or
--            merge the duplicates warning 3's query finds first, then clear.
--        (b) DELETE FROM <table> WHERE deleted_at IS NOT NULL;
--            destroys the retained config outright, and its blast radius is NOT symmetric across
--            the four: budget_categories.budget_id and .savings_goal_id are both ON DELETE
--            CASCADE, so deleting a budget or a goal destroys its categories too, and
--            savings_goal_contributions cascades off savings_goals -- money the user logged BY
--            HAND, recoverable from no provider. Deleting a boost only SET NULLs its
--            boost_executions. Step 1's dump records what was tombstoned, and nothing records
--            what the cascade took.
--   3. Re-run the rollback. The guard now finds nothing and proceeds, and step 1's dump is what
--      restores the classification on the way back up.
--
-- Idempotent: IF EXISTS on every statement, pg_constraint guards on the constraint restores.

-- ⚠ #1625 — the mechanism warning 1 asked for now EXISTS, and this file uses it.
--
-- The note above deferred instrumentation to "a future round" and specified what it should be: a
-- refusal on `deleted_at IS NOT NULL` rather than on a row count. That is what follows. The
-- collision-dependent abort described in warning 3 is real but PARTIAL -- it only fires when a
-- live and a tombstoned row happen to share a name -- so it was never the control the header
-- implied.
--
-- Reachability was established by EXECUTION, not by reading this file. Against a freshly rebuilt
-- `freshcheck` migrated to 192, holding one savings_goal with deleted_at IS NOT NULL and a name
-- no live row shared:
--     migrate -path backend/migrations -database $FRESHCHECK goto 157
-- ran 158/d without stopping. Observed afterwards: savings_goals.deleted_at gone from
-- information_schema.columns and the tombstoned goal still in savings_goals -- resurrected.
--
-- LOCK before COUNT, in this same DO block and transaction, on every table counted: a delete
-- committing between the counts and the DROP COLUMNs below would otherwise be dropped anyway.
-- Same finding as 000190's. The tables are locked in the same order they are counted.
DO $$
DECLARE
    n_goals   INTEGER;
    n_boosts  INTEGER;
    n_budgets INTEGER;
    n_cats    INTEGER;
BEGIN
    LOCK TABLE savings_goals IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE boosts IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE budgets IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE budget_categories IN ACCESS EXCLUSIVE MODE;

    SELECT COUNT(*) INTO n_goals   FROM savings_goals      WHERE deleted_at IS NOT NULL;
    SELECT COUNT(*) INTO n_boosts  FROM boosts             WHERE deleted_at IS NOT NULL;
    SELECT COUNT(*) INTO n_budgets FROM budgets            WHERE deleted_at IS NOT NULL;
    SELECT COUNT(*) INTO n_cats    FROM budget_categories  WHERE deleted_at IS NOT NULL;

    IF n_goals + n_boosts + n_budgets + n_cats > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000158: % savings_goals, % boosts, % budgets and % '
            'budget_categories row(s) are tombstoned (deleted_at IS NOT NULL). Dropping '
            'deleted_at retains every one of them but makes it indistinguishable from a live '
            'row, so config a user deleted reappears and its contributions count again.',
            n_goals, n_boosts, n_budgets, n_cats;
    END IF;
END $$;

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
