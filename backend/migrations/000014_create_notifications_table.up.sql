-- Migration: Create notifications table
-- Issue: #257 - Item Error Handler - Process ITEM Webhooks and User Notifications
-- Purpose: Store in-app and email notifications for users about item errors and other events
--
-- Related: Issue #202 - Plaid Webhook Integration
--
-- Notification Flow:
--   1. ItemErrorService creates notification when ITEM webhook processed
--   2. Notification includes action_url (e.g., re-authentication link for LOGIN_REQUIRED)
--   3. EmailService sends email notification (stub for MVP, real email in Phase 3+)
--   4. Frontend displays notifications and provides action buttons
--   5. User marks notification as read
--
-- Notification Types:
--   - item_error: General connection error
--   - login_required: User needs to re-authenticate
--   - pending_expiration: Connection expiring soon
--   - permission_revoked: User revoked access
--   - transaction_sync: Transaction sync status updates

CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL
        CHECK (type IN ('item_error', 'login_required', 'pending_expiration', 'permission_revoked', 'transaction_sync')),
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    action_url TEXT,
    action_label VARCHAR(100),
    read BOOLEAN DEFAULT FALSE,
    sent_email BOOLEAN DEFAULT FALSE,
    email_sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create indexes for efficient querying
-- Index for user's notification feed (ordered by date, most recent first)
CREATE INDEX idx_notifications_user_id ON notifications(user_id, created_at DESC);

-- Partial index for unread notifications (used for badge counts and filtering)
CREATE INDEX idx_notifications_unread ON notifications(user_id, read)
    WHERE read = FALSE;

-- Partial index for email worker to find unsent emails
CREATE INDEX idx_notifications_email ON notifications(sent_email, created_at)
    WHERE sent_email = FALSE;

-- Add table and column comments
COMMENT ON TABLE notifications IS 'User notifications for item errors, authentication requirements, and other events';
COMMENT ON COLUMN notifications.type IS 'Notification type for categorization and filtering';
COMMENT ON COLUMN notifications.action_url IS 'URL for user to take action (e.g., /accounts/reconnect?link_token=TOKEN&item_id=ITEM_ID)';
COMMENT ON COLUMN notifications.action_label IS 'Label for action button (e.g., "Reconnect Bank", "Reconnect Now")';
COMMENT ON COLUMN notifications.read IS 'Whether user has read this notification in-app';
COMMENT ON COLUMN notifications.sent_email IS 'Whether email notification was sent (stub for MVP)';
COMMENT ON COLUMN notifications.email_sent_at IS 'Timestamp when email was sent';
