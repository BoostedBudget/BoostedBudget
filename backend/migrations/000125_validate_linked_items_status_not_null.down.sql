-- Down migration for step 2 of 3 (Issue #1444): return the constraint to its NOT VALID state.
--
-- PostgreSQL has no `ALTER TABLE ... INVALIDATE CONSTRAINT`, so the only way to reverse a validation
-- is to drop the constraint and re-add it as NOT VALID. Both statements are catalog-only (the re-add
-- performs no scan precisely because it is NOT VALID), so this is fast and takes ACCESS EXCLUSIVE
-- only briefly.
--
-- Idempotent: DROP ... IF EXISTS, and the re-add is guarded.
ALTER TABLE linked_items DROP CONSTRAINT IF EXISTS linked_items_status_not_null;

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
