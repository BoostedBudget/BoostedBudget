-- Down migration for step 1 of 3 (Issue #1444): drop the NOT VALID CHECK added by the up migration.
-- Idempotent — IF EXISTS makes a re-run a no-op. By the time this runs, 000126's and 000125's down
-- migrations have already removed the NOT NULL and un-validated the constraint respectively, so this
-- is a plain catalog delete with no scan.
ALTER TABLE linked_items DROP CONSTRAINT IF EXISTS linked_items_status_not_null;
