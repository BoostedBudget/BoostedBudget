-- Rollback for 000154: drop the CHECK, then the column.
--
-- Dropping this column removes the ONLY queryable record of who removed an account. Any
-- provider-initiated removal written while 000154 was applied becomes indistinguishable from a user
-- disconnect, so a re-apply of 000154 leaves those rows NULL -- i.e. permanently ineligible for
-- automatic restore. That is the safe direction (an account stays removed rather than being
-- wrongly resurrected), and it is why this rollback is lossy rather than reversible. audit_logs
-- still records which path ran (account.removed vs account.removed_by_provider) for a human.
--
-- Idempotent: IF EXISTS on both statements.

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_removed_reason_valid;
ALTER TABLE accounts DROP COLUMN IF EXISTS removed_reason;
