-- Rollback: Remove transaction_past_due notification type
-- Restores 12-type constraint from migration 000049

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
