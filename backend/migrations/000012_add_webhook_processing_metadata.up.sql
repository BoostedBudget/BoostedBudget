-- Migration: Add webhook processing metadata for Issue #256
-- Purpose: Track webhook processing status, retries, and errors for TransactionSyncService
--
-- Related: Issue #256 - Transaction Sync Service
-- Builds on: Issue #202 - Plaid Webhook Integration
--
-- Processing Flow:
--   1. Webhook received and stored (Issue #202)
--   2. WebhookProcessor fetches unprocessed webhooks
--   3. TransactionSyncService processes webhook (marks processing_started_at)
--   4. On success: Mark processed=true, set processing_completed_at
--   5. On error: Increment retry_count, set next_retry_at (exponential backoff)
--   6. Max retries: 5 attempts with delays (1m, 5m, 15m, 1h, 6h)

-- Add processing metadata columns
ALTER TABLE webhook_events
ADD COLUMN processing_started_at TIMESTAMPTZ,
ADD COLUMN processing_completed_at TIMESTAMPTZ,
ADD COLUMN processing_error TEXT,
ADD COLUMN retry_count INTEGER DEFAULT 0 NOT NULL,
ADD COLUMN next_retry_at TIMESTAMPTZ;

-- Index for efficient unprocessed webhook queries
-- Filters: processed=false AND webhook_type='TRANSACTIONS' AND (next_retry_at IS NULL OR next_retry_at <= NOW())
-- Orders: created_at ASC (oldest first for FIFO processing)
CREATE INDEX idx_webhook_events_processing
ON webhook_events(webhook_type, processed, next_retry_at, created_at)
WHERE processed = false;

-- Comments for documentation and debugging
COMMENT ON COLUMN webhook_events.processing_started_at IS 'Timestamp when webhook processing began (NULL if never attempted). Used for debugging stuck webhooks.';
COMMENT ON COLUMN webhook_events.processing_completed_at IS 'Timestamp when webhook processing completed successfully (NULL if failed or not yet completed).';
COMMENT ON COLUMN webhook_events.processing_error IS 'Error message if processing failed. Used for debugging and manual investigation. NULL if no error.';
COMMENT ON COLUMN webhook_events.retry_count IS 'Number of processing attempts (0-5). Incremented after each failed attempt. Max 5 retries with exponential backoff: 1m, 5m, 15m, 1h, 6h.';
COMMENT ON COLUMN webhook_events.next_retry_at IS 'Scheduled timestamp for next retry attempt. NULL if no retry scheduled or max retries exceeded. Calculated using exponential backoff.';

-- Index documentation
COMMENT ON INDEX idx_webhook_events_processing IS 'Optimizes WebhookProcessor queries for unprocessed TRANSACTIONS webhooks with retry filtering. WHERE clause allows index-only scans for unprocessed webhooks.';
