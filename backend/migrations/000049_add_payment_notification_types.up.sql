-- Migration: Add payment and subscription lifecycle notification types
-- Issues #747 #748: Paddle webhook handlers create in-app notifications
--
-- Adds 6 new notification types:
-- - payment_failed: Recurring payment could not be processed
-- - subscription_past_due: Subscription payment is overdue
-- - subscription_paused: Subscription has been paused
-- - subscription_resumed: Subscription has been resumed
-- - subscription_activated: Subscription has been activated
-- - subscription_trialing: Subscription entered trialing period

-- Drop and re-add constraint with extended values (idempotent via DROP IF EXISTS)
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN (
        'item_error',
        'login_required',
        'pending_expiration',
        'permission_revoked',
        'transaction_sync',
        'budget_alert',
        'payment_failed',
        'subscription_past_due',
        'subscription_paused',
        'subscription_resumed',
        'subscription_activated',
        'subscription_trialing'
    ));

COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert, payment_failed, subscription_past_due, subscription_paused, subscription_resumed, subscription_activated, subscription_trialing';
