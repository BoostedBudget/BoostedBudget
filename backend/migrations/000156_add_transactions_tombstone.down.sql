-- Rollback for 000156: reverse ONLY what the up migration added.
--
-- Dropping these columns discards the tombstone records themselves, which is unavoidable: the rows
-- stay (they are no longer deleted by the sync path in this direction either), they simply become
-- indistinguishable from live rows again. That is strictly less destructive than the pre-000156
-- behaviour, which removed the rows outright.
--
-- Idempotent: IF EXISTS on every statement.

DROP INDEX IF EXISTS idx_transactions_tombstoned;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_deleted_reason_valid;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_tombstone_consistency;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_at;
