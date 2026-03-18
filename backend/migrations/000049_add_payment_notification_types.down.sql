-- Rollback: Restore original 6-type notification constraint
-- Removes payment and subscription lifecycle notification types

-- Drop any notifications with the new types before tightening constraint
DELETE FROM notifications WHERE type IN (
    'payment_failed',
    'subscription_past_due',
    'subscription_paused',
    'subscription_resumed',
    'subscription_activated',
    'subscription_trialing'
);

-- Restore original constraint
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN ('item_error', 'login_required', 'pending_expiration', 'permission_revoked', 'transaction_sync', 'budget_alert'));

COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert';
