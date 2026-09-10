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

-- ⚠ #1625 — this file now REFUSES rather than reasoning about the loss.
--
-- The header above argued the loss "fails safe" because a NULL cause is refused by
-- RestoreProviderRemovedAccount, and that argument is correct as far as it goes. It is not a
-- reason to let the rollback proceed: the cause is destroyed either way, and audit_logs records
-- the ACTION that ran, not the value this column held, so nothing reconstructs it. Removal cause
-- is exactly the "which universe does this retained row belong to" state the retention doctrine
-- protects.
--
-- Reachability was established by EXECUTION, not by reading this file. Against a freshly rebuilt
-- `freshcheck` migrated to 192, holding one account with status='removed' and
-- removed_reason='user_disconnect':
--     migrate -path backend/migrations -database $FRESHCHECK goto 153
-- ran 192/d ... 154/d without stopping. 000167's down is the only later refusal that reads this
-- column and it counts only removed_reason = 'link_removed', so a user_disconnect row passes
-- straight through it. Observed afterwards: removed_reason gone from
-- information_schema.columns, the account still present with status='removed'.
--
-- LOCK before COUNT, in this same DO block and therefore this same transaction: a bare COUNT(*)
-- takes only ACCESS SHARE, which a concurrent disconnect's ROW EXCLUSIVE does not conflict with,
-- so it could stamp a cause between the count and the DROP COLUMN below. Same finding as 000190's.
DO $$
DECLARE
    n INTEGER;
BEGIN
    LOCK TABLE accounts IN ACCESS EXCLUSIVE MODE;
    SELECT COUNT(*) INTO n FROM accounts WHERE removed_reason IS NOT NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000154: % accounts row(s) record WHY they were removed '
            '(removed_reason IS NOT NULL). This column is the only queryable record of that '
            'cause -- RestoreProviderRemovedAccount reads it to tell a bank-initiated removal '
            'from a user disconnect -- and dropping it destroys the distinction irrecoverably.', n;
    END IF;
END $$;

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_removed_reason_valid;
ALTER TABLE accounts DROP COLUMN IF EXISTS removed_reason;
