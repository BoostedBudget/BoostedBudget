-- Migration: Backfill user_accounts from existing accounts
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Create user_account entries for each unique (user_id, institution_id, mask) combination
--
-- NOTE: This migration is a NO-OP because there is no production data yet.
-- user_accounts will be created at runtime when accounts are linked via Plaid.
-- The service layer (UserAccountService.MatchOrCreate) handles creating user_accounts.
--
-- If we had existing data, this would:
-- 1. Group accounts by (user_id, institution_id, mask)
-- 2. Create a user_account for each unique combination
-- 3. Preserve customizations (nickname, color, etc.) from the account

-- =============================================================================
-- NO-OP Migration - user_accounts will be populated at runtime
-- =============================================================================

-- Intentionally empty - no backfill needed without existing data
