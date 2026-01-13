-- Migration: Simplify subscription tiers
-- Issues #674-#678: Remove free tier, switch to Paddle

-- Rename stripe_customer_id to paddle_customer_id
ALTER TABLE users RENAME COLUMN stripe_customer_id TO paddle_customer_id;

-- Update subscription_tier constraint to remove free tier and add trial status support
-- First drop the existing constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_tier;

-- Add new constraint with updated tiers (no free, add household tiers)
ALTER TABLE users ADD CONSTRAINT valid_subscription_tier
    CHECK (subscription_tier IN ('premium', 'household_2', 'household_3', 'household_4', 'household_5'));

-- Add 'trial' to valid subscription statuses if not already present
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;
ALTER TABLE users ADD CONSTRAINT valid_subscription_status
    CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial'));

-- Migrate existing free tier users to premium trial
UPDATE users
SET subscription_tier = 'premium',
    subscription_status = 'trial'
WHERE subscription_tier = 'free';

-- Migrate premium_lite users to premium
UPDATE users
SET subscription_tier = 'premium'
WHERE subscription_tier = 'premium_lite';

-- Migrate premium_plus users to household_5
UPDATE users
SET subscription_tier = 'household_5'
WHERE subscription_tier = 'premium_plus';
