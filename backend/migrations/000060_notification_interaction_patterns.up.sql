-- #952: Add interaction_pattern and transfer_suggestion_id to notifications
-- Add 4-pattern interaction_pattern column with CHECK constraint
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS interaction_pattern VARCHAR(20) NOT NULL DEFAULT 'informational';

-- Drop existing CHECK constraint before adding new one
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_interaction_pattern_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_interaction_pattern_check
    CHECK (interaction_pattern IN ('informational', 'action_required', 'acknowledgeable', 'accept_reject'));

-- Add nullable FK to transfer_suggestions for accept_reject notifications
-- Use NOT VALID + VALIDATE pattern to avoid SHARE ROW EXCLUSIVE lock on both tables (Squawk: adding-foreign-key-constraint)
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS transfer_suggestion_id UUID;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS fk_notifications_transfer_suggestion;
ALTER TABLE notifications ADD CONSTRAINT fk_notifications_transfer_suggestion
    FOREIGN KEY (transfer_suggestion_id) REFERENCES transfer_suggestions(id) NOT VALID;
ALTER TABLE notifications VALIDATE CONSTRAINT fk_notifications_transfer_suggestion;

-- Add transfer_suggestion to the notification type CHECK constraint
-- First drop the old constraint, then re-create with the new type
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check
    CHECK (type IN (
        'item_error', 'login_required', 'pending_expiration', 'permission_revoked',
        'transaction_sync', 'budget_alert', 'payment_failed', 'subscription_past_due',
        'subscription_cancelled', 'subscription_resumed', 'subscription_activated',
        'subscription_trialing', 'transaction_past_due', 'transfer_suggestion'
    ));

-- Backfill interaction_pattern for existing rows based on type
UPDATE notifications SET interaction_pattern = 'action_required'
    WHERE type IN ('login_required', 'pending_expiration', 'permission_revoked',
                   'payment_failed', 'subscription_past_due', 'transaction_past_due');

UPDATE notifications SET interaction_pattern = 'acknowledgeable'
    WHERE type = 'budget_alert';

-- All other existing rows keep the default 'informational'

COMMENT ON COLUMN notifications.interaction_pattern IS '#952: How the user interacts with this notification (informational, action_required, acknowledgeable, accept_reject)';
COMMENT ON COLUMN notifications.transfer_suggestion_id IS '#952: FK to transfer_suggestions table for accept_reject pattern notifications';
