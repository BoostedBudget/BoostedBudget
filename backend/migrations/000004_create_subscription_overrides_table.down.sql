-- Drop user_subscription_overrides table

DROP INDEX IF EXISTS idx_user_subscription_overrides_expires_at;
DROP INDEX IF EXISTS idx_user_subscription_overrides_user_id;
DROP TABLE IF EXISTS user_subscription_overrides;
