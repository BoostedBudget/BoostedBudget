-- #933: Revert removal of subscription_resumed notification type.
-- NOTE: The data remap (subscription_resumed → subscription_activated) is one-way
-- and is NOT reversed here. Only the schema constraint is restored.
-- This matches the 000057 down.sql precedent.
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
        'subscription_cancelled',
        'subscription_resumed',
        'subscription_activated',
        'subscription_trialing',
        'transaction_past_due',
        'transfer_suggestion',
        'transfer_suggestion_match'
    ));
COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert, payment_failed, subscription_past_due, subscription_cancelled, subscription_resumed, subscription_activated, subscription_trialing, transaction_past_due, transfer_suggestion, transfer_suggestion_match';
