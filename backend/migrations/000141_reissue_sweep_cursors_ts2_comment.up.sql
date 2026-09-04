-- Issue #1477 follow-up: RE-ISSUE the COMMENT on sweep_cursors.ts2, which became INCOMPLETE the day
-- a fourth sweep was added.
--
-- WHY THIS FILE EXISTS AT ALL. 000140 created sweep_cursors for THREE sweeps and its ts2 comment
-- ended "NULL for the two user sweeps". A fourth sweep (plaid_orphan_retry, the orphaned-Plaid-Item
-- retry) was then given the same persisted cursor, and it also leaves ts2 NULL -- so THREE sweeps do,
-- not two. The old text is not false about the sweeps it names; it is incomplete about the set, which
-- is the more durable kind of wrong because nothing in it looks stale. 000140 has already been applied
-- to running databases and golang-migrate never re-runs an applied version, so editing it in place
-- would leave every QA/staging catalogue carrying the short text forever. This migration is the
-- catch-up, exactly as 000136 was for 000135's two overstated retention comments.
--
-- THE NEW WORDING DESCRIBES A CONDITION, NOT A COUNT, AND THAT IS THE ACTUAL FIX. Enumerating sweeps
-- by number is what rotted: any wording of the form "NULL for the two/three/N sweeps" is invalidated
-- by the next sweep to be added, and the person adding it has no reason to look in a migration's
-- column comment. A comment that instead says WHEN ts2 is NULL -- whenever the owning sweep's sort key
-- has no second non-identity component -- stays true for every sweep that will ever exist, and points
-- at repository.SweepCursorName -- ONE OF TWO places, not the only one. That type enumerates the four
-- River-scheduled sweeps and is where a new one of THOSE must add itself; internal/database/
-- backfill_accounts_cursor.go independently writes a fifth row (accounts_user_account_backfill,
-- ts2 NULL) without using it. Both have to be checked.
--
-- ⚠ THAT CLAUSE IS CORRECTED IN PLACE: it read "the one place where the per-sweep TS1/TS2 mapping is
-- enumerated and where a new sweep must add itself anyway" -- the same claim the catalogue body below
-- retracts, left standing in the narrative when the body was fixed. Whether it was outright FALSE
-- turns on whether that fifth writer counts as a "sweep", and this tree calls it both ways
-- (backfill_accounts_cursor.go says "this sweep" of itself; SweepCursorName's own doc says those four
-- "are no longer every user of the table"). Under either reading, naming ONE place was wrong, and the
-- file contradicting its own catalogue text is wrong under all of them.
--
-- ⚠ THIS FILE'S OWN REPLACEMENT SENTENCE WAS LATER FOUND WRONG AND IS CORRECTED IN PLACE BELOW.
-- It read: "The authoritative per-sweep mapping is repository.SweepCursorName, whose constants each
-- name their own TS1/TS2 columns and which a new sweep has to edit anyway." A new writer does not
-- have to edit that type, and one already did not -- database.backfillAccountsUserAccountIDsWithDB
-- writes a fifth row without it (import cycle; see internal/database/backfill_accounts_cursor.go).
-- Migration 000184 re-issues the corrected body for databases that had already applied this file.
-- ⚠ The SAME claim also stood in this file's own NARRATIVE above ("the one place ... where a new
-- sweep must add itself anyway") and was missed on the first correction pass, because the guard
-- compares catalogue BODIES and never narrative. It is corrected there too, in place.
-- TestReissuedColumnCommentsDoNotDrift holds 000140, this file and 000184 in agreement -- comparing
-- the bodies with whitespace COLLAPSED, so re-wrapping is not drift but any change of word or
-- punctuation is. It is not a byte comparison, and the three files' indentation already differs.
--
-- SCOPE: one comment. No column, constraint, index, default or datum is touched. Squawk: COMMENT takes
-- only the catalogue lock it takes on any relation, rewrites nothing, and adds no constraint, so none
-- of the rules this repo excludes in .squawk.toml (require-concurrent-index-creation,
-- adding-not-nullable-field, adding-foreign-key-constraint, disallowed-unique-constraint, ...) is
-- applicable here rather than merely silenced.
--
-- Idempotent: COMMENT is inherently repeatable (setting the same comment twice leaves the same
-- comment), and the statement is guarded on the column still existing, so this is a clean no-op rather
-- than an error on a database where 000140 was rolled back.

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
