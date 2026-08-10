-- Down migration for step 3 of 3 (Issue #1444): drop NOT NULL and restore the validated CHECK, so
-- the schema returns to exactly the state 000125 left it in (constraint present and validated,
-- column nullable). Reversing in that order keeps the invariant continuously enforced: the CHECK is
-- in place before the column's own NOT NULL is removed, so there is no window in which a NULL could
-- be inserted.
--
-- The re-added CHECK is created already-valid (no NOT VALID) rather than NOT VALID: every row is
-- still non-NULL at this point, so the scan finds nothing and completes immediately. 000125's own
-- down migration is what returns it to NOT VALID, if the rollback continues that far.
--
-- Idempotent: both statements are guarded.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'linked_items'::regclass
          AND conname = 'linked_items_status_not_null'
    ) THEN
        ALTER TABLE linked_items
            ADD CONSTRAINT linked_items_status_not_null CHECK (status IS NOT NULL);
    END IF;
END $$;

ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL;
