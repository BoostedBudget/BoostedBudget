-- Rollback for 000159: reverse ONLY what the up migration added.
--
-- ⚠⚠ #1569 round 12 task 4 (fix round 1): same distinct hazard class as 000158's down.sql --
-- see the note there. This file RESURRECTS tombstoned custom_categories rows (makes them
-- indistinguishable from live ones) rather than DESTROYING anything; every row this migration
-- would touch is still physically present afterward. That is a different failure mode from the
-- REFUSE-while-rows-exist guards added elsewhere this round, and a plain row-count refusal is
-- the wrong tool for it -- deliberately left uninstrumented here, not a gap in the sweep.
--
-- READ THIS BEFORE ROLLING BACK.
--
--   1. Dropping deleted_at makes every tombstoned custom category VISIBLE AGAIN, all at once,
--      indistinguishable from live ones -- the classification lives only in these columns. A user
--      who deleted a category in the UI would find it back in every picker.
--   2. Under the post-000159 BINARY this rollback breaks the delete path outright:
--      custom_category_repository.DeleteWithReassignment issues `UPDATE custom_categories SET
--      deleted_at = ...`, which fails on a missing column, and every read filters on it. Roll the
--      application back first, or not at all.
--   3. Restoring the unconditional UNIQUE constraint can FAIL. If two rows share (user_id, name)
--      -- which the partial index permitted, one live and one tombstoned -- the ADD CONSTRAINT
--      below aborts. That is deliberate: silently dropping one of them would destroy user data on
--      a rollback. Resolve the duplicates by hand first:
--        SELECT user_id, name, count(*) FROM custom_categories GROUP BY 1,2 HAVING count(*) > 1;
--   4. This rollback does NOT resurrect the pre-#1569 behaviour of rewriting transaction history
--      on delete, and must not: the reverted rows are gone from no table, but a subsequent hard
--      DELETE of a category under the old binary would once again clear overrides and `reviewed`
--      on every transaction that used it.
--
-- If a rollback is genuinely required, dump
--   SELECT id, deleted_at, deleted_reason, deleted_by FROM custom_categories WHERE deleted_at IS NOT NULL
-- first, so the classification can be restored on the way back up.
--
-- Idempotent: IF EXISTS on every statement, pg_constraint guard on the constraint restore.

-- ⚠ #1625 — the mechanism warning 1 asked for now EXISTS, and this file uses it. See the same
-- note at the top of 000158's down for the shared reasoning; this is the custom_categories half.
--
-- Reachability was established by EXECUTION. Against a freshly rebuilt `freshcheck` migrated to
-- 192, holding one custom_category with deleted_at IS NOT NULL and a name no live row shared:
--     migrate -path backend/migrations -database $FRESHCHECK goto 158
-- ran 192/d ... 159/d without stopping -- no later down refuses on a category tombstone. Observed
-- afterwards: custom_categories.deleted_at gone from information_schema.columns and the tombstoned
-- category still in custom_categories, back in every picker. Warning 3's unique-constraint abort
-- did not fire, because the name did not collide, which is exactly why it was never a control.
--
-- LOCK before COUNT, same transaction, for the reason recorded on 000190's down.
DO $$
DECLARE
    n INTEGER;
BEGIN
    LOCK TABLE custom_categories IN ACCESS EXCLUSIVE MODE;
    SELECT COUNT(*) INTO n FROM custom_categories WHERE deleted_at IS NOT NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000159: % custom_categories row(s) are tombstoned '
            '(deleted_at IS NOT NULL). Dropping deleted_at retains those rows but makes them '
            'indistinguishable from live ones, so a category the user deleted reappears in every '
            'picker.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_custom_categories_tombstoned;

-- Restore the unconditional uniqueness. The partial unique index is dropped first so its name is
-- free for the constraint's implicit index to reuse.
DROP INDEX IF EXISTS unique_user_category_name;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'unique_user_category_name') THEN
        ALTER TABLE custom_categories ADD CONSTRAINT unique_user_category_name UNIQUE (user_id, name);
    END IF;
END $$;

ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS custom_categories_deleted_reason_valid;
ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS custom_categories_tombstone_consistency;

ALTER TABLE custom_categories DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE custom_categories DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE custom_categories DROP COLUMN IF EXISTS deleted_at;
