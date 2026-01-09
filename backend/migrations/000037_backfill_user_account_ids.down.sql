-- Rollback: Clear user_account_id from accounts and transactions
-- Issue: #171 - Plaid Re-Link Handling
--
-- This removes the backfilled user_account_id values, reverting to NULL.
-- Note: This should be run BEFORE 000036 rollback (which deletes user_accounts).

-- =============================================================================
-- Step 1: Clear user_account_id from transactions
-- =============================================================================

UPDATE transactions SET user_account_id = NULL WHERE user_account_id IS NOT NULL;

-- =============================================================================
-- Step 2: Clear user_account_id from accounts
-- =============================================================================

UPDATE accounts SET user_account_id = NULL WHERE user_account_id IS NOT NULL;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
