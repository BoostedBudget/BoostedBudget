-- #933: Remove subscription_resumed notification type (pause/resume not a supported product feature).
--
-- Paddle can still physically emit subscription.resumed (manual Paddle-side resume); the webhook
-- handler logs and acknowledges it as a no-op. This migration retires the type from the DB so
-- new inserts of subscription_resumed are rejected, bringing the DB CHECK into alignment with the
-- app-layer validTypes map in domain/notification.go (which already omits this type since #933).
--
-- Historical subscription_resumed rows are remapped to subscription_activated (preserving the
-- positive "welcome back" semantics) rather than subscription_cancelled.
-- Pattern: mirrors migration 000057 (which retired subscription_paused the same way).

UPDATE notifications SET type = 'subscription_activated' WHERE type = 'subscription_resumed';

-- Drop and re-add constraint without subscription_resumed.
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
        'subscription_activated',
        'subscription_trialing',
        'transaction_past_due',
        'transfer_suggestion',
        'transfer_suggestion_match'
    ));
COMMENT ON COLUMN notifications.type IS 'Notification type: item_error, login_required, pending_expiration, permission_revoked, transaction_sync, budget_alert, payment_failed, subscription_past_due, subscription_cancelled, subscription_activated, subscription_trialing, transaction_past_due, transfer_suggestion, transfer_suggestion_match';
