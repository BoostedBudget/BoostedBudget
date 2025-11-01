-- Migration: Add error tracking to linked_items table
-- Issue: #257 - Item Error Handler - Process ITEM Webhooks and User Notifications
-- Purpose: Track connection errors, status, and retry logic for linked items
--
-- Related: Issue #202 - Plaid Webhook Integration
--
-- Error Flow:
--   1. ITEM webhook received (ERROR, LOGIN_REQUIRED, PENDING_EXPIRATION, USER_PERMISSION_REVOKED)
--   2. ItemErrorService updates linked_items status and error details
--   3. Notification created for user
--   4. For LOGIN_REQUIRED: Generate update mode link token for re-authentication
--
-- Status Values:
--   - active: Item is working normally
--   - error: General connection error
--   - login_required: User needs to re-authenticate (credentials changed, MFA required)
--   - pending_expiration: Item access expiring soon
--   - revoked: User revoked access at their bank

-- Add status and error tracking columns
ALTER TABLE linked_items
ADD COLUMN status VARCHAR(50) DEFAULT 'active'
    CHECK (status IN ('active', 'error', 'login_required', 'pending_expiration', 'revoked')),
ADD COLUMN error_code VARCHAR(100),
ADD COLUMN error_message TEXT,
ADD COLUMN last_error_at TIMESTAMPTZ,
ADD COLUMN retry_count INTEGER DEFAULT 0,
ADD COLUMN next_retry_at TIMESTAMPTZ;

-- Create indexes for efficient querying
-- Index for filtering by status and finding items needing attention
CREATE INDEX idx_linked_items_status ON linked_items(status, last_error_at);

-- Partial index for retry scheduling (only rows that need retry)
CREATE INDEX idx_linked_items_retry ON linked_items(next_retry_at)
    WHERE next_retry_at IS NOT NULL;

-- Add column comments for documentation
COMMENT ON COLUMN linked_items.status IS 'Connection status: active, error, login_required, pending_expiration, revoked';
COMMENT ON COLUMN linked_items.error_code IS 'Plaid error code (e.g., ITEM_LOGIN_REQUIRED)';
COMMENT ON COLUMN linked_items.error_message IS 'Human-readable error message';
COMMENT ON COLUMN linked_items.last_error_at IS 'Timestamp of most recent error';
COMMENT ON COLUMN linked_items.retry_count IS 'Number of failed sync attempts (for transaction sync retries, separate from webhook retries)';
COMMENT ON COLUMN linked_items.next_retry_at IS 'Scheduled time for next sync retry attempt';
