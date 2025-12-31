-- Rollback: Rename clerk_user_id back to auth0_user_id
-- Part of Auth0 -> Clerk migration (Issue #593)

-- Rename column in users table back
ALTER TABLE users RENAME COLUMN clerk_user_id TO auth0_user_id;

-- Rename column in user_identities table back
ALTER TABLE user_identities RENAME COLUMN clerk_user_id TO auth0_user_id;

-- Rename indexes back
ALTER INDEX IF EXISTS idx_users_clerk_user_id RENAME TO idx_users_auth0_user_id;
ALTER INDEX IF EXISTS idx_user_identities_clerk_user_id RENAME TO idx_user_identities_auth0_user_id;

-- Restore original column comments
COMMENT ON COLUMN users.auth0_user_id IS 'Auth0 user identifier from JWT sub claim';
COMMENT ON COLUMN user_identities.auth0_user_id IS 'Auth0 identity (e.g., google-oauth2|123456)';
