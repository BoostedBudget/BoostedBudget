-- Migration rollback: Remove webhook processing metadata
-- Related: Issue #256 - Transaction Sync Service

-- Drop index first
DROP INDEX IF EXISTS idx_webhook_events_processing;

-- Drop columns
ALTER TABLE webhook_events
DROP COLUMN IF EXISTS processing_started_at,
DROP COLUMN IF EXISTS processing_completed_at,
DROP COLUMN IF EXISTS processing_error,
DROP COLUMN IF EXISTS retry_count,
DROP COLUMN IF EXISTS next_retry_at;
