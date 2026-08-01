-- Issue #1444, step 1 of 3: add a NOT VALID CHECK asserting linked_items.status is never NULL.
--
-- WHY THREE MIGRATIONS INSTEAD OF ONE `SET NOT NULL`
--
-- A plain `ALTER TABLE ... ALTER COLUMN status SET NOT NULL` performs a MANDATORY full-table
-- validation scan while holding ACCESS EXCLUSIVE, blocking every read and write to linked_items for
-- the scan's duration. The first draft of this migration did exactly that, and argued the scan was
-- cheap "because 000123 already made every row non-NULL". That reasoning is wrong: the scan still
-- reads every row regardless of whether it finds a violation. Found by a Codex adversarial review
-- of this branch.
--
-- It would also have violated a convention THIS SAME BRANCH introduced (Issue #1464,
-- .claude/skills/db-migration-guide/SKILL.md): never hold ACCESS EXCLUSIVE across expensive work.
-- Shipping a violation of a freshly-written rule is how conventions rot, and a future contributor
-- copying this file as the in-repo example of "how we add NOT NULL" would inherit the anti-pattern.
--
-- The three-step transition avoids the blocking scan entirely (PostgreSQL 12+):
--   000124 (this file)  ADD CONSTRAINT ... CHECK (status IS NOT NULL) NOT VALID
--                       -> instant. Takes ACCESS EXCLUSIVE but performs NO scan, so the lock is
--                          held only for a catalog write.
--   000125              VALIDATE CONSTRAINT
--                       -> performs the scan under SHARE UPDATE EXCLUSIVE, which does NOT block
--                          concurrent reads or writes. This is the entire point of the split.
--   000126              SET NOT NULL -- PostgreSQL 12+ proves non-nullness from the already-VALIDATED
--                          CHECK and SKIPS the scan -- then DROP the now-redundant CHECK.
--
-- The steps MUST be separate migrations, not separate statements in one file: golang-migrate wraps
-- each migration in a single transaction, so combining them would hold this file's ACCESS EXCLUSIVE
-- right through 000125's scan, reproducing exactly the hazard being avoided.
--
-- Independent of migration 000032's linked_items_valid_status CHECK
-- (status IN ('active','error','login_required','pending_expiration','revoked','replaced','disconnected')),
-- which is vacuously satisfied by NULL and remains satisfied by every non-NULL row. The two
-- constraints are additive, not competing.
--
-- Idempotent: guarded on pg_constraint, so a re-run is a no-op.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'linked_items'::regclass
          AND conname = 'linked_items_status_not_null'
    ) THEN
        ALTER TABLE linked_items
            ADD CONSTRAINT linked_items_status_not_null CHECK (status IS NOT NULL) NOT VALID;
    END IF;
END $$;
