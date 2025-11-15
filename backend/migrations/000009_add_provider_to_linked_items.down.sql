-- Rollback: Remove provider column and related objects from linked_items
-- WARNING: This will delete provider information for all linked items
-- Only use in development or with full backup

-- Drop index
DROP INDEX IF EXISTS idx_linked_items_provider;

-- Drop constraint
ALTER TABLE linked_items DROP CONSTRAINT IF EXISTS valid_provider;

-- Drop column (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'provider'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN provider;
    END IF;
END $$;
