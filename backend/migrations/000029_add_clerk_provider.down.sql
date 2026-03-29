-- Rollback: Remove 'clerk' from valid_provider CHECK constraint
-- WARNING: This will fail if any rows have provider='clerk'

-- Drop the new constraint
ALTER TABLE user_identities DROP CONSTRAINT IF EXISTS valid_provider;

-- Restore original constraint (without 'clerk')
ALTER TABLE user_identities ADD CONSTRAINT valid_provider
  CHECK (provider IN ('google-oauth2', 'microsoft', 'apple', 'github', 'auth0'));
