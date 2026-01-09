-- Migration: Add user_account_id to balance_snapshots
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Enable balance history to be queried by stable user_account reference,
--          preserving history across Plaid re-links
--
-- When a user re-links their bank account:
-- 1. Old account gets marked as 'replaced' with a new account_id
-- 2. Without user_account_id, balance history would be lost
-- 3. With user_account_id, history can be queried across all account versions

-- =============================================================================
-- Add user_account_id column to balance_snapshots
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'balance_snapshots' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE balance_snapshots
        ADD COLUMN user_account_id UUID REFERENCES user_accounts(id) ON DELETE SET NULL;
    END IF;
END $$;

-- =============================================================================
-- Create index for efficient user_account_id queries
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_balance_snapshots_user_account_id
ON balance_snapshots(user_account_id);

-- Create composite index for date range queries by user_account
CREATE INDEX IF NOT EXISTS idx_balance_snapshots_user_account_date
ON balance_snapshots(user_account_id, snapshot_at);

-- =============================================================================
-- Backfill user_account_id from accounts table
-- =============================================================================

UPDATE balance_snapshots bs
SET user_account_id = a.user_account_id
FROM accounts a
WHERE bs.account_id = a.id
  AND bs.user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;
