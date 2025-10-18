-- Rollback users table creation
-- Migration: 000001_create_users_table.down.sql

-- Drop trigger
DROP TRIGGER IF EXISTS update_users_updated_at ON users;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop indexes
DROP INDEX IF EXISTS idx_users_deletion_scheduled;
DROP INDEX IF EXISTS idx_users_is_deleted;
DROP INDEX IF EXISTS idx_users_created_at;
DROP INDEX IF EXISTS idx_users_subscription_tier;
DROP INDEX IF EXISTS idx_users_primary_region;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_auth0_user_id;

-- Drop table
DROP TABLE IF EXISTS users;
