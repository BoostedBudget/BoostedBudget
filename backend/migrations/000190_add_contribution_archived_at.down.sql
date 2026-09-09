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
-- seventeen other reversible-guard downs in this tree.
--
-- To roll back deliberately: decide, per goal, whether the archived rows should count, then clear
-- archived_at (or delete the rows) yourself. There is no correct automatic answer -- see 000190's
-- "NO BACKFILL, DELIBERATELY" note for why guessing is destructive in the other direction too.

DO $$
DECLARE
    n INTEGER;
BEGIN
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
