-- Migration: Add paddle_subscription_id to users table
-- Issue #48: Subscription Management UI
-- Purpose: Store Paddle subscription ID for each user to enable subscription management operations

-- Add paddle_subscription_id column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS paddle_subscription_id VARCHAR(255);

-- Add index for faster lookups by subscription ID
CREATE INDEX IF NOT EXISTS idx_users_paddle_subscription_id ON users(paddle_subscription_id);

-- Add comment for documentation
COMMENT ON COLUMN users.paddle_subscription_id IS 'Paddle subscription ID for user''s active subscription (e.g., sub_01h...)';
