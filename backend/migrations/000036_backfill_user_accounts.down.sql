-- Rollback: Remove backfilled user_accounts
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This will delete all user_accounts created by the backfill migration.
-- Any user customizations (nickname, custom_color, etc.) will be lost!

-- Delete all user_accounts (cascade should handle dependent FKs if any)
-- Note: This only works if no other data depends on user_accounts
DELETE FROM user_accounts;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
