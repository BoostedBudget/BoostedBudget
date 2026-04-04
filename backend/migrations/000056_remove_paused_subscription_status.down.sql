-- Restore paused status value
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial', 'trialing', 'paused'));
