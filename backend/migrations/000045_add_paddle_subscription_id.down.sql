-- Migration Rollback: Remove paddle_subscription_id from users table
-- Issue #48: Subscription Management UI

-- Remove index first
DROP INDEX IF EXISTS idx_users_paddle_subscription_id;

-- Remove column
ALTER TABLE users DROP COLUMN IF EXISTS paddle_subscription_id;
