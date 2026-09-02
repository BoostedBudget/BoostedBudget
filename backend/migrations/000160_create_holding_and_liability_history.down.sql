-- Rollback for 000160_create_holding_and_liability_history (#1569 T6).
-- Both tables are pure history written beside unchanged current state, so dropping them removes
-- the retained past without touching holdings / credit_liabilities / mortgage_liabilities /
-- student_loan_liabilities, which keep their replace semantics either way.
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH (#1569 round 12 task 4, Codex-cited example).
-- holding_snapshots (daily investment positions) and liability_statements (captured statement
-- terms and APRs) are both history Plaid does not backfill or re-report once its own window has
-- moved on. Same mechanism as 000150/000151/000096/000163/000180: REFUSE while either table holds
-- rows rather than silently destroy them.

DO $$
DECLARE
    n_holdings BIGINT;
    n_liab     BIGINT;
BEGIN
    SELECT COUNT(*) INTO n_holdings FROM holding_snapshots;
    SELECT COUNT(*) INTO n_liab     FROM liability_statements;
    IF n_holdings > 0 OR n_liab > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000160: % holding_snapshots row(s) and % liability_statements '
            'row(s) exist, and this rollback DROPs both tables. They are the only record of past '
            'investment positions and captured statement terms/APRs; Plaid does not backfill or '
            're-report either once its own window has moved on. Export them, or empty them '
            'deliberately, before retrying.', n_holdings, n_liab;
    END IF;
END $$;

DROP TABLE IF EXISTS liability_statements CASCADE;
DROP TABLE IF EXISTS holding_snapshots CASCADE;
