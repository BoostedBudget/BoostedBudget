-- Reverse 000184 by restoring the two sweep_cursors comments as 000141 and 000142 ORIGINALLY shipped
-- them, verbatim -- the same convention 000141, 000142, 000136 and 000129 follow: "down" for a
-- comment-only migration means putting the previous wording back, not dropping the comment or the
-- column it describes (both are owned by 000140 and its own down migration).
--
-- READ THIS BEFORE RUNNING IT. The two texts restored below are the wordings 000184 exists to
-- replace, and each carries a claim that was checked and found wrong: the ts2 text names
-- repository.SweepCursorName as the authority "which a new sweep has to edit anyway" (a fifth writer
-- already does not), and the exclusive text states the over-cap sacrifice with no precondition (it
-- holds only while the group's first cap-worth keeps failing). Rolling this back reinstates both.
-- Roll back only as part of rolling 000184 out of the tree, never as a way of "restoring" the column
-- documentation.
--
-- ⚠ DO NOT grep 000141 OR 000142 FOR THE TEXT BELOW -- IT IS NO LONGER THERE. Both files' own up
-- migrations were corrected in place in the same commit that added 000184 (see 000140's header for
-- the doctrine), so "as they ORIGINALLY shipped" means the text those migrations APPLIED to the
-- databases this rollback targets, not the text those files hold today. That is also why this
-- rollback must keep the old wording: on an already-migrated database it is the genuine previous
-- catalogue state. On a database created from the corrected files it is not a reversal but a
-- regression, so run it only as part of rolling this migration out of the tree.
--
-- Guarded exactly as up.sql is, so this is a no-op rather than an error on a database where 000140
-- was rolled back first and the table or column is gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sweep_cursors'
          AND column_name = 'ts2'
    ) THEN
        COMMENT ON COLUMN sweep_cursors.ts2 IS
        'Second non-identity ORDER BY component of the owning sweep, and NULL whenever that sweep''s '
        'sort key has no second non-identity component -- which is the case for every sweep whose '
        'candidates are ordered by a single timestamp plus the row id. Stated as a CONDITION rather '
        'than as a list or a count of sweeps on purpose: the original wording said "NULL for the two '
        'user sweeps" and a fourth sweep made it incomplete the day it was added, while nothing in it '
        'looked stale. The authoritative per-sweep mapping is repository.SweepCursorName, whose '
        'constants each name their own TS1/TS2 columns and which a new sweep has to edit anyway. '
        'A NULL here is a REAL position, not "unset" (absence of the whole row is "unset"), and is '
        'coalesced to the timestamptz infinity by the page queries to match ORDER BY ... NULLS LAST.';
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sweep_cursors'
          AND column_name = 'exclusive'
    ) THEN
        COMMENT ON COLUMN sweep_cursors.exclusive IS
        'FALSE = resume AT this timestamp key, re-visiting the rows that tie on it and skipping none. '
        'TRUE = resume STRICTLY AFTER it, and it is set in exactly one condition: the run started from '
        'a stored key and spent its whole cap without the timestamp key changing, so an inclusive save '
        'would hand the next run the identical page forever. THE PRICE, STATED AS A CONDITION SO IT '
        'CANNOT BE MISREAD AS A DELAY: whichever rows sit behind the first cap-worth of that one '
        'identical-timestamp-key group are then reached on no run at all -- not on a later run, and '
        'not by restarting the sweep from the beginning, because restarting re-reads that same first '
        'cap-worth and stops in the same place. 000140 said those rows were reached "until the next '
        'wrap"; that recovery does not exist and this comment retracts it. Everything RANKED BEHIND '
        'the group is reached, which is what the exclusive advance buys. The authoritative statement, '
        'with the test that demonstrates it, is repository.NextPosition.';
    END IF;
END $$;
