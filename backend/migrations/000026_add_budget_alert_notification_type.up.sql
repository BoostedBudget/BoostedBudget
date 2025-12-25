-- Migration: Add budget_alert notification type
-- Issue: #36 - Rule Executors - Budget alert notifications
-- Purpose: Allow notifications of type 'budget_alert' for automation rule alerts

-- Drop the existing CHECK constraint
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;

-- Add new CHECK constraint with budget_alert included
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN ('item_error', 'login_required', 'pending_expiration', 'permission_revoked', 'transaction_sync', 'budget_alert'));

-- Update comment to reflect new type
COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert';
