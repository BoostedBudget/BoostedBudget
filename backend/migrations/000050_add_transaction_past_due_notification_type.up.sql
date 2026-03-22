-- Migration: Add transaction_past_due notification type
-- Issues #747 #748 gap remediation: transaction.past_due webhook handler needs this type

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
        'subscription_trialing',
        'transaction_past_due'
    ));

COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert, payment_failed, subscription_past_due, subscription_paused, subscription_resumed, subscription_activated, subscription_trialing, transaction_past_due';
