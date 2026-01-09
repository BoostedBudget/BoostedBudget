-- Migration: Add user_account_id to transactions
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Provide stable reference for querying transactions across re-links
--
-- The user_account_id column allows querying transactions by a stable identifier
-- that doesn't change when Plaid's account_id changes on re-link.
--
-- Note: This migration depends on 000031_create_user_accounts_table

-- =============================================================================
-- Step 1: Add Column (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Add user_account_id FK (stable reference across re-links)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE transactions ADD COLUMN user_account_id UUID REFERENCES user_accounts(id);
    END IF;
END $$;

-- =============================================================================
-- Step 2: Create Indexes for Performance
-- =============================================================================

-- Primary index for querying transactions by user_account
CREATE INDEX IF NOT EXISTS idx_transactions_user_account
    ON transactions(user_account_id);

-- Composite index for user_account + date (common query pattern)
CREATE INDEX IF NOT EXISTS idx_transactions_user_account_date
    ON transactions(user_account_id, date DESC);

-- =============================================================================
-- Step 3: Add Column Comment for Documentation
-- =============================================================================

COMMENT ON COLUMN transactions.user_account_id IS 'FK to user_accounts. Stable reference surviving Plaid re-links. Use this for querying transactions across account re-links.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
