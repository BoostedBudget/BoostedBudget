-- Issue #668: Add cleanup tracking columns to linked_items
-- These columns track when items are scheduled for automatic cleanup
-- due to being in error state, revoked, or stale (not synced)

-- Add cleanup tracking columns
DO $$ BEGIN
    -- cleanup_notified_at: When user was notified about pending cleanup
    ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS cleanup_notified_at TIMESTAMPTZ;

    -- cleanup_scheduled_at: When automatic cleanup is scheduled to run
    -- This is set to cleanup_notified_at + grace_period (7 days)
    ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS cleanup_scheduled_at TIMESTAMPTZ;
EXCEPTION
    WHEN duplicate_column THEN NULL;
END $$;

-- Add comments for documentation
COMMENT ON COLUMN linked_items.cleanup_notified_at IS 'Timestamp when user was notified about pending cleanup due to error/stale/revoked status';
COMMENT ON COLUMN linked_items.cleanup_scheduled_at IS 'Timestamp when automatic cleanup will execute (typically 7 days after notification)';

-- Index for cleanup queries: find items scheduled for cleanup
-- Partial index only includes rows where cleanup is scheduled
CREATE INDEX IF NOT EXISTS idx_linked_items_cleanup_scheduled
    ON linked_items(cleanup_scheduled_at)
    WHERE cleanup_scheduled_at IS NOT NULL;

-- Index for finding cleanup candidates: items in error status for extended periods
-- Uses the existing status and last_error_at columns
CREATE INDEX IF NOT EXISTS idx_linked_items_cleanup_candidates
    ON linked_items(status, last_error_at)
    WHERE status IN ('error', 'login_required', 'revoked')
    AND cleanup_scheduled_at IS NULL;
