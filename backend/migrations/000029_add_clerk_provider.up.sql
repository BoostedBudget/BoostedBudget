-- Migration: Add 'clerk' to valid_provider CHECK constraint
-- Part of Auth0 -> Clerk migration (Issue #593)
--
-- The original constraint only allowed Auth0 providers:
--   ('google-oauth2', 'microsoft', 'apple', 'github', 'auth0')
--
-- This migration adds 'clerk' for native Clerk authentication

-- Drop the old constraint
ALTER TABLE user_identities DROP CONSTRAINT IF EXISTS valid_provider;

-- Add new constraint including 'clerk'
ALTER TABLE user_identities ADD CONSTRAINT valid_provider
  CHECK (provider IN ('google-oauth2', 'microsoft', 'apple', 'github', 'auth0', 'clerk'));
