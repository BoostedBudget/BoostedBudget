-- Rollback: Drop user_identities table and restore auth0_user_id unique constraint
-- WARNING: This will delete ALL multi-provider identity mappings
-- Only use in development or with full backup

DROP TABLE IF EXISTS user_identities CASCADE;

-- Restore unique constraint on auth0_user_id
ALTER TABLE users ADD CONSTRAINT users_auth0_user_id_key UNIQUE (auth0_user_id);
