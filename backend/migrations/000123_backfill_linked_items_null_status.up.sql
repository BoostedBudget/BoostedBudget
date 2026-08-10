-- Issue #1444: backfill NULL linked_items.status to 'active' ahead of the follow-up migration
-- (000124) that adds a NOT NULL constraint.
--
-- linked_items.status has been nullable since migration 000013 added it with a DEFAULT 'active'
-- but no NOT NULL constraint. Since PostgreSQL 11, `ADD COLUMN ... DEFAULT` backfills every
-- pre-existing row at ALTER time, so a NULL status is unreachable through any production write
-- path today (linked_item_repository.go's Create omits `status` from its column list entirely,
-- always getting the column default) — this migration is expected to affect 0 rows on most
-- databases. It exists to close the class of latent scan-failure/logic bugs a genuinely NULL
-- status can still cause (Issue #1444: several repository scans and one EXISTS-predicate guard
-- treated a NULL status incorrectly before this issue's code fixes), and to make the follow-up
-- NOT NULL migration (000124) safe to apply unconditionally.
--
-- NULL is treated as "active" here to match the established rule already encoded in
-- normalizeLinkedItemStatus (backend/internal/repository/linked_item_repository.go) and
-- liveLinkedItemExistsClause (backend/internal/repository/profile_repository.go): a NULL status
-- has always meant "this item is fully live," so backfilling it to the literal 'active' value is
-- a data correction, not a behavior change.
--
-- Deliberately its own migration, split from 000124's ALTER TABLE ... SET NOT NULL, per this
-- repo's DDL/backfill split convention (db-migration-guide skill, Issue #1464): a full-table
-- UPDATE sharing a transaction with a NOT NULL-adding ALTER would hold that ALTER's validation
-- scan (ACCESS EXCLUSIVE) for the combined duration of both statements. Splitting into two
-- migrations means each runs, commits, and releases its own locks independently.
--
-- Idempotent: WHERE status IS NULL means a second run finds nothing left to backfill — a pure
-- no-op.
UPDATE linked_items
SET status = 'active',
    updated_at = NOW()
WHERE status IS NULL;
