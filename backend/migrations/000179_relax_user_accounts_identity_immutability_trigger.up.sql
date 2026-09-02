-- #1569 round 12 task 1b: relax trg_user_accounts_identity_immutable (migration 000177) so it
-- forbids `value -> different value` but ALLOWS `NULL -> value`.
--
-- THE BUG. Migration 000177's trigger function used `NEW.col IS DISTINCT FROM OLD.col`, which is
-- true for a NULL -> value transition exactly as it is for a value -> different-value transition.
-- That is stricter than the feature's own intended semantic ("immutable ONCE SET", per
-- 000177's header and TestNoUpdateOfUserAccountsIdentityColumns's doc comment) and has two
-- independently-confirmed consequences:
--   1. Migration 000177's OWN backfill UPDATE (Step 2 of that migration) is a NULL -> value write.
--      Re-executing 000177's .up.sql against an already-migrated, populated database — which round
--      12 task 1 made ./scripts/test-migrations.sh's idempotency/data-integrity suites actually do —
--      finds any row that acquired NULL identity after the first run and the trigger (created in
--      Step 3, AFTER the backfill, so it never blocked the FIRST run) rejects the second run's
--      identical backfill write. Confirmed live: TEST 1 of test-migrations.sh turned RED on 000177.
--   2. A user_accounts row with NO physical account ever pointing at it (identity_type IS NULL —
--      migration 000177's header calls this an "orphan") can therefore never have its identity set,
--      even by a deliberate future repair — the trigger blocks the NULL -> value "claim" write
--      unconditionally. FindByInstitutionAndMask's orphan wildcard (migration 000178's narrowing)
--      is documented as a residual risk for exactly this reason.
--
-- THE FIX'S EXACT SEMANTIC: "immutable once set", where "set" is judged for the ROW as a whole
-- (via identity_type), not independently per column. Concretely:
--   - If OLD.identity_type IS NULL, the row has never been minted: ANY write to identity_type
--     and/or identity_subtype is allowed (this is the NULL -> value first-time claim the fix
--     exists to permit — including setting a genuinely NULL subtype, e.g. type='other').
--   - If OLD.identity_type IS NOT NULL, the row has already been minted: changing EITHER column to
--     a different value (including NULL -> value) is rejected.
--
-- WHY THE GATE IS identity_type ALONE, NOT "does this column's own old value happen to be NULL".
-- identity_type and identity_subtype are always written together as one unit — at INSERT time by
-- UserAccountRepository.Create and database/backfill.go's synthesiseUserAccountID, and by this very
-- migration's backfill — never independently. So identity_type IS NULL is an accurate proxy for
-- "this row has never been minted" for BOTH columns. Using each column's own old value as its gate
-- instead would open exactly the back door the round-12 brief asked to be reasoned about: a row
-- minted with a genuinely, PERMANENTLY NULL subtype (identity_type='other', identity_subtype=NULL —
-- a real, valid mint outcome, not an unclaimed orphan) would stay "claimable" forever, letting a
-- later write silently attach a fabricated subtype after the row's identity was already frozen.
-- Gating both columns on identity_type's OLD value closes that door: once identity_type is set,
-- identity_subtype is frozen too, NULL or not.
--
-- WHAT THIS DOES NOT CHANGE. This migration touches ONLY the trigger function
-- (CREATE OR REPLACE, naturally idempotent). It does not touch the AST guard
-- (TestNoUpdateOfUserAccountsIdentityColumns) — that guard forbids ANY `UPDATE user_accounts`
-- statement in backend/internal Go from touching these columns at all, unconditionally, and stays
-- exactly that strict: the relaxation here is a migration/backfill-path capability, not a grant to
-- application code. It also does NOT claim an orphan's identity anywhere — MatchOrCreate gains no
-- new write path in this migration. Making the trigger permit a NULL -> value claim only makes that
-- future repair POSSIBLE at the database level; it does not perform it.
CREATE OR REPLACE FUNCTION enforce_user_accounts_identity_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.identity_type IS NOT NULL
       AND (NEW.identity_type IS DISTINCT FROM OLD.identity_type
            OR NEW.identity_subtype IS DISTINCT FROM OLD.identity_subtype) THEN
        RAISE EXCEPTION
            '#1569: user_accounts.identity_type/identity_subtype are frozen once set and cannot be changed (id=%)',
            OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
