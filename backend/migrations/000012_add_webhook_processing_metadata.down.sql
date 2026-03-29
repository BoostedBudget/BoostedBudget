-- Rollback: Remove webhook processing metadata columns
-- WARNING: This will delete all webhook processing status and retry information
-- Only use in development or with full backup

-- Drop index
DROP INDEX IF EXISTS idx_webhook_events_processing;

-- Drop columns (idempotent)
DO $$
BEGIN
    -- Drop processing_started_at if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'webhook_events' AND column_name = 'processing_started_at'
    ) THEN
        ALTER TABLE webhook_events DROP COLUMN processing_started_at;
    END IF;

    -- Drop processing_completed_at if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'webhook_events' AND column_name = 'processing_completed_at'
    ) THEN
        ALTER TABLE webhook_events DROP COLUMN processing_completed_at;
    END IF;

    -- Drop processing_error if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'webhook_events' AND column_name = 'processing_error'
    ) THEN
        ALTER TABLE webhook_events DROP COLUMN processing_error;
    END IF;

    -- Drop retry_count if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'webhook_events' AND column_name = 'retry_count'
    ) THEN
        ALTER TABLE webhook_events DROP COLUMN retry_count;
    END IF;

    -- Drop next_retry_at if exists
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'webhook_events' AND column_name = 'next_retry_at'
    ) THEN
        ALTER TABLE webhook_events DROP COLUMN next_retry_at;
    END IF;
END $$;
