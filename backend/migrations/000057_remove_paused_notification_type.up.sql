-- #930: Remove subscription_paused notification type (no longer used)
-- Note: existing subscription_paused notifications in DB are historical records and remain valid.
-- We only tighten the constraint for new inserts.
-- Any existing rows with type='subscription_paused' must be updated first to avoid constraint violation.
UPDATE notifications SET type = 'subscription_cancelled' WHERE type = 'subscription_paused';
-- Drop and re-add constraint without subscription_paused
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
        'transaction_past_due'
    ));
COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert, payment_failed, subscription_past_due, subscription_cancelled, subscription_resumed, subscription_activated, subscription_trialing, transaction_past_due';
