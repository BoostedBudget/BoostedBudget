-- Rollback: Remove error tracking columns from linked_items
-- WARNING: This will delete all item error status and retry information
-- Only use in development or with full backup

-- Drop indexes
DROP INDEX IF EXISTS idx_linked_items_status;
DROP INDEX IF EXISTS idx_linked_items_retry;

-- Drop columns (idempotent)
DO $$
BEGIN
    -- Drop status column if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'status'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN status;
    END IF;

    -- Drop error_code if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'error_code'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN error_code;
    END IF;

    -- Drop error_message if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'error_message'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN error_message;
    END IF;

    -- Drop last_error_at if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'last_error_at'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN last_error_at;
    END IF;

    -- Drop retry_count if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'retry_count'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN retry_count;
    END IF;

    -- Drop next_retry_at if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'next_retry_at'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN next_retry_at;
    END IF;
END $$;
