-- Migration: Rename auth0_user_id to clerk_user_id
-- Part of Auth0 -> Clerk migration (Issue #593)

-- Rename column in users table
ALTER TABLE users RENAME COLUMN auth0_user_id TO clerk_user_id;

-- Rename column in user_identities table
ALTER TABLE user_identities RENAME COLUMN auth0_user_id TO clerk_user_id;

-- Rename indexes
ALTER INDEX IF EXISTS idx_users_auth0_user_id RENAME TO idx_users_clerk_user_id;
ALTER INDEX IF EXISTS idx_user_identities_auth0_user_id RENAME TO idx_user_identities_clerk_user_id;

-- Update column comments
COMMENT ON COLUMN users.clerk_user_id IS 'Clerk user identifier from JWT sub claim';
COMMENT ON COLUMN user_identities.clerk_user_id IS 'External identity provider user ID (e.g., user_xxx for Clerk)';
