-- Rollback for 000163 (#1569 Task 11). Dropping the ledger loses the record of which days the
-- repair examined and which it refused; the repaired portfolio_snapshots rows themselves are NOT
-- reverted, because the pre-repair values were wrong and this table is the only copy of them.
--
-- ⚠⚠ #1569 round 12 task 4: "take a copy before rolling back if that history matters" was a
-- comment, not a control. Same mechanism as 000150/000151/000096/000180: REFUSE while rows exist
-- rather than rely on an operator reading SQL comments from a CLI that prints none of them.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM portfolio_snapshot_repairs;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000163: portfolio_snapshot_repairs holds % row(s), and this '
            'rollback DROPs the table. It is the only record of which days a portfolio-snapshot '
            'repair examined and what it did; nothing reconstructs it. Export it, or empty it '
            'deliberately, before retrying.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_portfolio_snapshot_repairs_outcome;
DROP TABLE IF EXISTS portfolio_snapshot_repairs;
