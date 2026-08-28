-- #1569 round 12 task 1b: revert to migration 000177's stricter trigger function, which rejects
-- NULL -> value transitions as well as value -> different-value transitions.
--
-- HONESTY CHECK, per the round-12 brief: restoring the stricter function does NOT retroactively
-- reject any data already written while the relaxed version was in effect — a trigger only fires on
-- a future UPDATE, so no existing row is touched or invalidated by running this down migration.
-- What DOES change is future behaviour: any UPDATE that relies on the NULL -> value permission this
-- migration's .up.sql added (a re-run of 000177's own backfill against an already-migrated
-- database, or a future orphan-identity-repair write) would once again be rejected by
-- `IS DISTINCT FROM`, reintroducing exactly the round-12 defect this migration exists to fix. Do
-- not roll this back while relying on either of those paths.
CREATE OR REPLACE FUNCTION enforce_user_accounts_identity_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.identity_type IS DISTINCT FROM OLD.identity_type
       OR NEW.identity_subtype IS DISTINCT FROM OLD.identity_subtype THEN
        RAISE EXCEPTION
            '#1569: user_accounts.identity_type/identity_subtype are frozen at mint and cannot be updated (id=%)',
            OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
