-- Rollback for 000096_create_portfolio_snapshots
-- Drops the portfolio_snapshots table and the covering index added to investment_transactions.
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH (#1569 round 12 task 4). portfolio_snapshots is the
-- only record of what a portfolio was worth (value, cost basis, gain/loss) on a past day -- there
-- is no provider to re-derive it from. Same mechanism as 000150/000151/000016/000017/000180: fail
-- loudly rather than silently destroy, conditioned on rows existing so the rollback suite's
-- clean_database stays unaffected.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM portfolio_snapshots;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000096: portfolio_snapshots holds % row(s), and this rollback '
            'DROPs the table. That is the only record of what a portfolio was worth on a past day; '
            'nothing reconstructs it. Export it, or empty it deliberately, before retrying.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_inv_txn_user_date_id;
DROP TABLE IF EXISTS portfolio_snapshots CASCADE;
