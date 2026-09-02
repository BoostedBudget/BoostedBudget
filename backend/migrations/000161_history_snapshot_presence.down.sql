-- Rollback for 000161_history_snapshot_presence (#1569 T6 fix round 1).
-- Dropping `present` returns the tables to 000160's positive-only shape, in which a confirmed
-- liquidation cannot be expressed and a same-day clear leaves the day over-reporting.

DROP INDEX IF EXISTS idx_holding_snapshots_user_date_present;

ALTER TABLE holding_snapshots DROP COLUMN IF EXISTS present;
ALTER TABLE liability_statements DROP COLUMN IF EXISTS present;
