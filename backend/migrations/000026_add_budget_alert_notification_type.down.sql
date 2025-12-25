-- Rollback: Remove budget_alert notification type
-- Issue: #36 - Rule Executors

-- Drop the new CHECK constraint
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;

-- Restore original CHECK constraint without budget_alert
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN ('item_error', 'login_required', 'pending_expiration', 'permission_revoked', 'transaction_sync'));

-- Restore original comment
COMMENT ON COLUMN notifications.type IS 'Notification type for categorization and filtering';
