-- Rollback: Restore original 5-value subscription status constraint
-- Migrates any paused→inactive, trialing→trial before tightening constraint

-- Migrate status values that won't fit the old constraint
UPDATE users SET subscription_status = 'inactive' WHERE subscription_status = 'paused';
UPDATE users SET subscription_status = 'trial'    WHERE subscription_status = 'trialing';

-- Restore original constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial'));
