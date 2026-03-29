-- Rollback: Drop dashboard preferences table
-- Issue #354: Cash Flow Analysis + Customizable Dashboard

-- Drop the cash flow index on transactions (keep the table, just remove our index)
DROP INDEX IF EXISTS idx_transactions_cashflow;

-- Drop the dashboard preferences table and related indexes
DROP TABLE IF EXISTS user_dashboard_preferences CASCADE;
