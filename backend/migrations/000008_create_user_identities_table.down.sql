-- Rollback: Remove user_identities table and restore original single-identity constraint
-- WARNING: This migration will:
--   1. Delete all multi-provider identity mappings
--   2. Restore UNIQUE constraint on users.auth0_user_id (prevents multiple social logins per user)
--   3. Break any code expecting UUID-based user_id in middleware

-- Drop user_identities table
-- This removes all identity mappings and multi-provider support
DROP TABLE IF EXISTS user_identities;

-- Restore UNIQUE constraint on users.auth0_user_id
-- This reverts to single-identity-per-user model
ALTER TABLE users ADD CONSTRAINT users_auth0_user_id_key UNIQUE (auth0_user_id);

-- Note: users.auth0_user_id field is never removed during up migration,
-- so it still contains the original Auth0 user ID and can be restored safely.
