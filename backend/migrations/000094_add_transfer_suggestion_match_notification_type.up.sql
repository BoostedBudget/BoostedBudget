-- #972: Add transfer_suggestion_match to notifications_type_check.
--
-- GAP U fix: the fuzzy-match correlation path emits notifications with
-- Type="transfer_suggestion_match" (domain.NotificationTypeTransferSuggestionMatch).
-- Migration 000060 created notifications_type_check but only included the original
-- "transfer_suggestion" type. Without this migration the INSERT fails with SQLSTATE 23514.

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN (
        'item_error', 'login_required', 'pending_expiration', 'permission_revoked',
        'transaction_sync', 'budget_alert', 'payment_failed', 'subscription_past_due',
        'subscription_cancelled', 'subscription_resumed', 'subscription_activated',
        'subscription_trialing', 'transaction_past_due', 'transfer_suggestion',
        'transfer_suggestion_match'
    ));
