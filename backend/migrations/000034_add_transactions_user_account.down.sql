-- Rollback: Remove user_account_id from transactions
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This rollback will DELETE:
-- - user_account_id references on all transactions
-- - Ability to query transactions by stable account reference
--
-- Recommended: Only rollback in development or with confirmed database backup

-- =============================================================================
-- Step 1: Drop Indexes (Idempotent)
-- =============================================================================

DROP INDEX IF EXISTS idx_transactions_user_account;
DROP INDEX IF EXISTS idx_transactions_user_account_date;

-- =============================================================================
-- Step 2: Drop Column (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE transactions DROP COLUMN user_account_id;
    END IF;
END $$;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
