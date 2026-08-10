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
-- at repository.SweepCursorName, the one place where the per-sweep TS1/TS2 mapping is enumerated and
-- where a new sweep must add itself anyway.
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
        'looked stale. The authoritative per-sweep mapping is repository.SweepCursorName, whose '
        'constants each name their own TS1/TS2 columns and which a new sweep has to edit anyway. '
        'A NULL here is a REAL position, not "unset" (absence of the whole row is "unset"), and is '
        'coalesced to the timestamptz infinity by the page queries to match ORDER BY ... NULLS LAST.';
    END IF;
END $$;
