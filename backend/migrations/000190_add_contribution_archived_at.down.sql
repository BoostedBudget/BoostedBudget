-- Reverses 000190.
--
-- ⚠ REFUSES while any row is archived. archived_at is the ONLY record that a contribution belongs
-- to a CLOSED contributions period; nothing else in the schema can distinguish such a row from a
-- live one (that is exactly why 000190 had to add a column rather than reuse
-- managed_by_reconciliation). Dropping it therefore does not "return to the pre-#1625 behaviour" --
-- it destroys state created SINCE this migration and silently resurrects the 100 + 100 = 200
-- double-count for every goal that round-tripped its progress_basis while 000190 was applied.
-- A derived financial figure quietly becoming wrong is precisely the failure class #1625 exists to
-- remove, so this refuses rather than proceeds.
--
-- ⚠ An earlier version of this file dropped the column unconditionally and claimed in a comment
-- that the resulting double-count was "the pre-#1625 behaviour this reverts to, not new damage".
-- That was false, and it was found by adversarial review, not by a guard.
--
-- Same shape as 000180's refusal on the SAME table while any row carries deleted_at, and as the
-- other reversible-guard downs in this tree. Re-derive that set rather than trusting a number in
-- prose:
--
--   /usr/bin/grep -l "refusing to roll back" backend/migrations/*.down.sql
--
-- ⚠ #1625 third fix wave F5: this line carried a count, and the count was wrong. It said
-- "seventeen other" while naming 000180 separately, so it claimed 19 guarded downs including this
-- one; the phrase matches 18 files, i.e. SIXTEEN other than 000180 and this one. Two separate
-- verifications confirmed the wrong number because both counted RAISE EXCEPTION (20 files,
-- including unguarded downs that raise for other reasons) instead of the guard phrase the
-- sentence is about. The number is gone rather than corrected: it goes stale on the next guarded
-- down, and nothing executes it.
--
-- To roll back deliberately: decide, per goal, whether the archived rows should count, then clear
-- archived_at (or delete the rows) yourself. There is no correct automatic answer -- see 000190's
-- "NO BACKFILL, DELIBERATELY" note for why guessing is destructive in the other direction too.

-- ⚠ The lock is load-bearing, not defensive tidiness (#1625 second fix wave M4). Without it this
-- guard was TOCTOU-racy: the COUNT(*) took only ACCESS SHARE, which does not conflict with the
-- ROW EXCLUSIVE the production archive path (savings_contribution_repository.ArchiveByGoal) takes,
-- so an archive could commit between the count and the ALTER below acquiring its own ACCESS
-- EXCLUSIVE — and the marker the guard had just decided did not exist was dropped anyway, with the
-- rollback reporting success. golang-migrate runs a migration file in ONE transaction, so taking
-- the lock here holds it through the ALTER and closes the window.
--
-- On an empty table this costs nothing and the rollback still proceeds; a concurrent writer is
-- what it is meant to stop. Pinned by
-- TestDownMigration000190_RefusalIsNotTOCTOURacy_Integration, which observes both the lock itself
-- and a second connection being blocked by it.
DO $$
DECLARE
    n INTEGER;
BEGIN
    LOCK TABLE savings_goal_contributions IN ACCESS EXCLUSIVE MODE;
    SELECT COUNT(*) INTO n FROM savings_goal_contributions WHERE archived_at IS NOT NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000190: % savings_goal_contributions row(s) are archived '
            '(archived_at IS NOT NULL). Dropping the column discards the only record that those '
            'rows belong to a CLOSED contributions period, so the goals they belong to would '
            'resume double-counting their progress.', n;
    END IF;
END $$;

ALTER TABLE savings_goal_contributions
    DROP COLUMN IF EXISTS archived_at;
