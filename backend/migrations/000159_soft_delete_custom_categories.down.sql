-- Rollback for 000159: reverse ONLY what the up migration added.
--
-- ⚠⚠ #1569 round 12 task 4 (fix round 1), PARTLY RETRACTED IN PLACE by #1625: same distinct
-- hazard class as 000158's down.sql -- see the note there. This file RESURRECTS tombstoned
-- custom_categories rows (makes them indistinguishable from live ones) rather than DESTROYING
-- anything; every row this migration would touch is still physically present afterward. That is a
-- different failure mode from the REFUSE-while-rows-exist guards added elsewhere in round 12, and
-- a plain row-count refusal is still the wrong tool for it. All of that stands.
--
-- WHAT DOES NOT, and what this note said until #1625: "deliberately left uninstrumented here, not
-- a gap in the sweep". This file IS instrumented. The DO block below refuses on
-- `deleted_at IS NOT NULL` -- the resurrection-shaped mechanism round 12 asked a future round to
-- build, NOT the destruction-guard pattern it correctly warned against reaching for unmodified.
-- A reader who trusted the retracted sentence would conclude that no control exists here and that
-- building one was still an open decision; both are false. Retracted rather than delegated to
-- 000158's header, because a reader of THIS file gets the wrong answer from THIS file.
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
-- HOW TO ACTUALLY PROCEED, if a rollback is genuinely required. ⚠ This used to say only "dump the
-- classification first", which is step 1 of 3 and, on its own, an instruction that CANNOT WORK:
-- the DO block below refuses while a single tombstone exists, so a dump leaves the rollback
-- exactly as blocked as it was. The steps that do work:
--
--   1. Dump the classification, so step 2 stays reversible:
--        SELECT id, deleted_at, deleted_reason, deleted_by FROM custom_categories
--        WHERE deleted_at IS NOT NULL
--   2. Empty the predicate the guard refuses on. Two ways, both lossy:
--        (a) UPDATE custom_categories SET deleted_at = NULL, deleted_reason = NULL, deleted_by = NULL
--            WHERE deleted_at IS NOT NULL;
--            keeps every row and RESURRECTS it -- warning 1. It also makes warning 3 live, and
--            EARLIER than warning 3 says: unique_user_category_name is a LIVE-only partial unique
--            index (WHERE deleted_at IS NULL), so this UPDATE itself aborts on the first
--            resurrected row whose (user_id, name) already exists live -- before the unconditional
--            UNIQUE restored further down is ever reached. Rename or merge the duplicates warning
--            3's query finds first, then clear.
--        (b) DELETE FROM custom_categories WHERE deleted_at IS NOT NULL;
--            destroys the categories outright and frequently CANNOT RUN: transactions'
--            custom_category_override_id is ON DELETE RESTRICT, so any category still named by a
--            transaction override refuses, and budget_categories is ON DELETE CASCADE, so the
--            ones that do delete take budget rows with them.
--   3. Re-run the rollback. The guard now finds nothing and proceeds, and step 1's dump is what
--      restores the classification on the way back up.
--
-- Idempotent: IF EXISTS on every statement, pg_constraint guard on the constraint restore.

-- ⚠ #1625 — the mechanism the retraction above describes, in full. This is the custom_categories
-- half of the same change made to 000158's down; the reasoning is restated in both headers rather
-- than kept in one, so neither file has to be read to understand the other.
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
