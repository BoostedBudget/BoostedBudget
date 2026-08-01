-- Issue #1444: add NOT NULL to linked_items.status, removing the entire class of NULL-status
-- bugs the issue's code fixes had to work around (nullable-column-scanned-into-non-pointer-field
-- panics, and a NULL-unsafe `<>` predicate in DeleteUserCascade's un-cleaned-Plaid-item guard).
--
-- Deliberately its own migration, split from 000123's backfill UPDATE, per this repo's
-- DDL/backfill split convention (db-migration-guide skill, Issue #1464) — see 000123's header
-- comment for the full rationale. By the time this migration runs, 000123 has already committed
-- and every row's status is non-NULL, so this ALTER's mandatory full-table validation scan (which
-- ALTER TABLE ... SET NOT NULL always performs, holding ACCESS EXCLUSIVE for its duration) finds
-- nothing to reject and completes quickly, without also having to hold that lock across a
-- simultaneous backfill.
--
-- Does NOT conflict with the linked_items_valid_status CHECK constraint added by migration 000032
-- (`status IN ('active', 'error', 'login_required', 'pending_expiration', 'revoked', 'replaced',
-- 'disconnected')`) — that CHECK is satisfied by every NULL row (CHECK constraints are vacuously
-- satisfied by NULL) and remains satisfied by every non-NULL row after this migration; the two
-- constraints are independent and additive, not competing.
--
-- Idempotent: SET NOT NULL is a no-op if the column is already NOT NULL (Postgres does not error
-- on a redundant SET NOT NULL), but this is still guarded explicitly for clarity and to match this
-- repo's convention of checking information_schema before an ALTER.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items'
          AND column_name = 'status'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE linked_items ALTER COLUMN status SET NOT NULL;
    END IF;
END $$;
