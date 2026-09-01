-- Issue #1569 round 20 fix 1: RE-ISSUE the COMMENTs on sweep_cursors.ts2 and sweep_cursors.exclusive,
-- because 000141 and 000142 -- themselves corrections -- each shipped with a claim of their own that
-- does not survive checking. This is the third link in that chain. The chain is mechanically enforced
-- by TestReissuedColumnCommentsDoNotDrift, which PREDATES this migration -- it was written for the
-- 000149/000174/000175 accounts.removed_at incident, the same failure one table over -- and which
-- this migration was added to rather than caused.
--
-- WHAT WAS WRONG IN 000141's ts2 TEXT, RESTATED IN FULL SO THIS FILE IS NOT A DANGLING POINTER:
--
--     "The authoritative per-sweep mapping is repository.SweepCursorName, whose constants each name
--      their own TS1/TS2 columns and which a new sweep has to edit anyway."
--
-- A new writer does NOT have to edit that type, and one already did not. #1569 round 14 added a fifth
-- row to this table, "accounts_user_account_backfill", written by
-- database.backfillAccountsUserAccountIDsWithDB; internal/repository's own integration tests import
-- internal/database, so the reverse import is a cycle and the backfill restates the contract locally
-- in internal/database/backfill_accounts_cursor.go instead. It writes ts2 = NULL explicitly
-- (saveBackfillAccountsCursor) and never mentions SweepCursorName. 000141 replaced a COUNT with a
-- CONDITION and was right to; it then named a single authority that was already not the only one.
-- The replacement below names both places and says plainly that neither is sufficient alone.
--
-- WHAT WAS WRONG IN 000142's exclusive TEXT, RESTATED IN FULL:
--
--     "whichever rows sit behind the first cap-worth of that one identical-timestamp-key group are
--      then reached on no run at all"
--
-- Stated with no precondition, which overstates it. The sacrifice lasts only while that first
-- cap-worth of rows keeps FAILING, because failing is what makes them keep consuming the whole cap;
-- the moment they succeed they leave the candidate set and the tail behind them becomes the new head
-- and is reached. repository.NextPosition already carries this precondition and flags that its own
-- earlier wording got it wrong ("AND THE PRECONDITION IS NOT WHAT THIS COMMENT USED TO SAY"), so the
-- catalogue was the last place still missing it. Nothing about the MECHANISM changes: everything
-- ranked behind the group is still reached unconditionally, which is what the exclusive advance buys.
--
-- WHY A NEW FILE RATHER THAN ONLY AN EDIT -- AND WHY BOTH. Correcting a COMMENT that has already been
-- applied takes TWO edits, and this repo has now been bitten three times by doing only one of them.
-- The ORIGINAL files (000140, and 000141/000142 which are originals for their own new sentences) are
-- what a FRESH database gets and have been edited in place in the same commit as this file; THIS file
-- is what every already-migrated database gets, since golang-migrate never re-runs an applied
-- version. On a fresh database the two paths set the same catalogue text either way, which is what
-- TestReissuedColumnCommentsDoNotDrift asserts -- per column, not per file, and the four files do not
-- each carry both bodies: the ts2 body is in 000140, 000141 and this file; the exclusive body is in
-- 000140, 000142 and this file; only 000140 and this file carry both. ⚠ The guard compares the bodies
-- with whitespace COLLAPSED (normaliseCommentText), so re-wrapping across lines is not drift but any
-- change of word or punctuation is. It is NOT a byte comparison and must not be described as one --
-- the copies' indentation differs between a top-level statement and one inside a DO block.
--
-- SCOPE: two comments. No column, constraint, index, default, type or datum is touched, and no
-- behaviour changes -- the mechanism was always correct, only its description was not. Squawk: this
-- repo runs squawk with excluded_rules set, so a clean report here would be weak evidence and is not
-- being leaned on. Reasoned instead from the operation: COMMENT ON COLUMN takes only the ordinary
-- catalogue lock on the relation, rewrites no heap or index, adds no constraint, sets no default and
-- validates nothing, so every rule this repo excludes (require-concurrent-index-creation,
-- adding-not-nullable-field, adding-foreign-key-constraint, disallowed-unique-constraint, ...) is
-- INAPPLICABLE to these statements rather than merely silenced by configuration.
--
-- Idempotent: COMMENT is inherently repeatable, and each statement is guarded on its column still
-- existing, so this is a clean no-op rather than an error on a database where 000140 was rolled back.

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
        'looked stale. The per-sweep mapping is enumerated by repository.SweepCursorName, whose '
        'constants each name their own TS1/TS2 columns -- but that type is NOT every writer of this '
        'table and a new writer does not have to edit it: database.backfillAccountsUserAccountIDsWithDB '
        'writes a fifth row, accounts_user_account_backfill, without using it (an import cycle forces a '
        'local restatement in internal/database/backfill_accounts_cursor.go), and that row stores ts2 '
        'NULL as well. Check both places, not just the type. A NULL here is a REAL position, not '
        '"unset" (absence of the whole row is "unset"), and is coalesced to the timestamptz infinity '
        'by the page queries to match ORDER BY ... NULLS LAST.';
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
        'cap-worth and stops in the same place. THE PRECONDITION IS THE HEAD, NOT THE GROUP, and this '
        'comment used to omit it: the sacrifice lasts only while that first cap-worth keeps FAILING and '
        'so keeps consuming the whole cap. The moment those head rows succeed they leave the candidate '
        'set, the tail behind them becomes the new head, and it is reached. 000140 said the sacrificed '
        'rows were reached "until the next wrap"; that recovery does not exist and this comment '
        'retracts it. Everything RANKED BEHIND the group is reached unconditionally, which is what the '
        'exclusive advance buys. The authoritative statement, with the precondition and the test that '
        'constructs it, is repository.NextPosition.';
    END IF;
END $$;
