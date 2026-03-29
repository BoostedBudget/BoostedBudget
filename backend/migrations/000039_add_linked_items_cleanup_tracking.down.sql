-- Issue #668: Rollback cleanup tracking columns from linked_items

-- Drop indexes first (if they exist)
DROP INDEX IF EXISTS idx_linked_items_cleanup_scheduled;
DROP INDEX IF EXISTS idx_linked_items_cleanup_candidates;

-- Remove cleanup tracking columns
DO $$ BEGIN
    ALTER TABLE linked_items DROP COLUMN IF EXISTS cleanup_scheduled_at;
    ALTER TABLE linked_items DROP COLUMN IF EXISTS cleanup_notified_at;
EXCEPTION
    WHEN undefined_column THEN NULL;
END $$;
