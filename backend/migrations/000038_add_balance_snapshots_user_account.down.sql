-- Rollback: Remove user_account_id from balance_snapshots
-- Issue: #171 - Plaid Re-Link Handling

-- =============================================================================
-- Drop indexes
-- =============================================================================

DROP INDEX IF EXISTS idx_balance_snapshots_user_account_date;
DROP INDEX IF EXISTS idx_balance_snapshots_user_account_id;

-- =============================================================================
-- Drop column
-- =============================================================================

ALTER TABLE balance_snapshots DROP COLUMN IF EXISTS user_account_id;
