-- Rollback: Restore old subscription tier structure
-- Issues #674-#678

-- Rename paddle_customer_id back to stripe_customer_id
ALTER TABLE users RENAME COLUMN paddle_customer_id TO stripe_customer_id;

-- Restore old tier constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_tier;
ALTER TABLE users ADD CONSTRAINT valid_subscription_tier
    CHECK (subscription_tier IN ('free', 'premium_lite', 'premium', 'premium_plus'));

-- Restore old status constraint (remove trial)
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due'));

-- Note: Cannot automatically restore user tier assignments
-- Manual intervention may be required
