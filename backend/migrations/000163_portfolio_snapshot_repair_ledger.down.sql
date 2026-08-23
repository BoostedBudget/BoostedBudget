-- Rollback for 000163 (#1569 Task 11). Dropping the ledger loses the record of which days the
-- repair examined and which it refused; the repaired portfolio_snapshots rows themselves are NOT
-- reverted, because the pre-repair values were wrong and this table is the only copy of them.
-- Take a copy before rolling back if that history matters.

DROP INDEX IF EXISTS idx_portfolio_snapshot_repairs_outcome;
DROP TABLE IF EXISTS portfolio_snapshot_repairs;
