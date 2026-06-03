-- #930: Remove "paused" subscription status — subscriptions are either active or cancelled
-- Migrate any existing paused users to cancelled first
UPDATE users SET subscription_status = 'cancelled' WHERE subscription_status = 'paused';
-- Drop and re-add constraint without paused (idempotent via DROP IF EXISTS)
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial', 'trialing'));
