-- Rollback: Remove user_account_id from holdings and investment_transactions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency

-- =============================================================================
-- PART 1: holdings
-- =============================================================================

ALTER TABLE holdings DROP CONSTRAINT IF EXISTS fk_holdings_user_account_id;
DROP INDEX IF EXISTS idx_holdings_user_account_id;
ALTER TABLE holdings DROP COLUMN IF EXISTS user_account_id;

-- =============================================================================
-- PART 2: investment_transactions
-- =============================================================================

ALTER TABLE investment_transactions DROP CONSTRAINT IF EXISTS fk_inv_txn_user_account_id;
DROP INDEX IF EXISTS idx_inv_txn_user_account_id;
ALTER TABLE investment_transactions DROP COLUMN IF EXISTS user_account_id;
