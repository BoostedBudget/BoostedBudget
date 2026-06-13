-- Rollback for 000096_create_portfolio_snapshots
-- Drops the portfolio_snapshots table and the covering index added to investment_transactions.

DROP INDEX IF EXISTS idx_inv_txn_user_date_id;
DROP TABLE IF EXISTS portfolio_snapshots CASCADE;
