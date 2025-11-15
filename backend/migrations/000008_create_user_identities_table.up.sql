-- Migration: Create user_identities table for multi-provider authentication support
-- This migration implements Phase 2 of Issue #148
-- Enables:
--   1. Multiple Auth0 identities per user (Google + Microsoft + Apple, etc.)
--   2. Clean UUID-based user_id for external APIs (fixes Plaid pipe character bug)
--   3. Provider-agnostic identity management
--
-- IMPORTANT: This migration has no automated rollback.
-- Manual rollback procedure documented in 000008_ROLLBACK.md

-- Create user_identities table
CREATE TABLE user_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth0_user_id VARCHAR(255) NOT NULL UNIQUE,  -- e.g., 'google-oauth2|123456'
    provider VARCHAR(50) NOT NULL,                -- e.g., 'google-oauth2', 'microsoft', 'apple'
    provider_user_id VARCHAR(255) NOT NULL,       -- e.g., '123456' (extracted from auth0_user_id)
    email VARCHAR(255),                           -- Email from this provider (may differ across providers)
    email_verified BOOLEAN DEFAULT FALSE,         -- Email verification status from provider
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,    -- Primary identity for this user
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMP WITH TIME ZONE,        -- Track when this identity was last used for login

    -- Constraints
    CONSTRAINT unique_user_provider UNIQUE(user_id, provider),  -- One identity per provider per user
    CONSTRAINT valid_provider CHECK (provider IN ('google-oauth2', 'microsoft', 'apple', 'github', 'auth0'))
);

-- Performance indexes
CREATE INDEX idx_user_identities_user_id ON user_identities(user_id);              -- Lookup all identities for a user
CREATE INDEX idx_user_identities_auth0_user_id ON user_identities(auth0_user_id);  -- Auth0 ID -> user lookup (hot path)
CREATE INDEX idx_user_identities_email ON user_identities(email);                  -- Email-based identity lookup
CREATE UNIQUE INDEX idx_user_identities_primary ON user_identities(user_id) WHERE is_primary = TRUE;  -- Ensure one primary per user

-- Trigger for updated_at timestamp
CREATE TRIGGER update_user_identities_updated_at
    BEFORE UPDATE ON user_identities
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Migrate existing users to user_identities table
-- All existing auth0_user_id entries become primary identities
INSERT INTO user_identities (user_id, auth0_user_id, provider, provider_user_id, email, email_verified, is_primary, created_at)
SELECT
    id,                                                           -- user_id
    auth0_user_id,                                               -- auth0_user_id
    CASE                                                          -- Extract provider from auth0_user_id prefix
        WHEN auth0_user_id LIKE 'google-oauth2|%' THEN 'google-oauth2'
        WHEN auth0_user_id LIKE 'microsoft|%' THEN 'microsoft'
        WHEN auth0_user_id LIKE 'windowslive|%' THEN 'microsoft' -- Legacy Microsoft provider
        WHEN auth0_user_id LIKE 'apple|%' THEN 'apple'
        WHEN auth0_user_id LIKE 'github|%' THEN 'github'
        ELSE 'auth0'                                             -- Username/password auth
    END,
    SUBSTRING(auth0_user_id FROM POSITION('|' IN auth0_user_id) + 1),  -- Extract provider_user_id after pipe
    email,                                                       -- Email from users table
    email_verified,                                              -- Email verification status
    TRUE,                                                        -- Mark existing identities as primary
    created_at                                                   -- Preserve original creation timestamp
FROM users
WHERE auth0_user_id IS NOT NULL;

-- Remove UNIQUE constraint from users.auth0_user_id
-- This field is now deprecated in favor of user_identities table
-- Kept for backward compatibility during transition period
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_auth0_user_id_key;

-- Add comment to document the migration
COMMENT ON TABLE user_identities IS 'Maps Auth0 identities to internal user UUIDs. Supports multiple auth providers per user (Google, Microsoft, Apple, etc.). Implements Issue #148 Phase 2.';
