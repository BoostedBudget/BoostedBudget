-- #972: Revert transfer_suggestion_match from notifications_type_check.
-- Remove rows with the new type first; otherwise the restored CHECK will reject them.
DELETE FROM notifications WHERE type = 'transfer_suggestion_match';
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN (
        'item_error', 'login_required', 'pending_expiration', 'permission_revoked',
        'transaction_sync', 'budget_alert', 'payment_failed', 'subscription_past_due',
        'subscription_cancelled', 'subscription_resumed', 'subscription_activated',
        'subscription_trialing', 'transaction_past_due', 'transfer_suggestion'
    ));
