-- Rollback: Drop user_subscription_overrides table
-- WARNING: This will delete ALL custom subscription limits (enterprise deals, promos)
-- Only use in development or with full backup

DROP TABLE IF EXISTS user_subscription_overrides CASCADE;
