-- Rollback: Remove interaction_pattern, transfer_suggestion_id from notifications

-- Drop transfer_suggestion notifications before tightening type constraint
DELETE FROM notifications WHERE type = 'transfer_suggestion';

-- Remove transfer_suggestion from type constraint
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN (
        'item_error', 'login_required', 'pending_expiration', 'permission_revoked',
        'transaction_sync', 'budget_alert', 'payment_failed', 'subscription_past_due',
        'subscription_cancelled', 'subscription_resumed', 'subscription_activated',
        'subscription_trialing', 'transaction_past_due'
    ));

-- Drop interaction_pattern constraint and column
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_interaction_pattern_check;
ALTER TABLE notifications DROP COLUMN IF EXISTS interaction_pattern;

-- Drop transfer_suggestion_id FK and column
ALTER TABLE notifications DROP COLUMN IF EXISTS transfer_suggestion_id;
