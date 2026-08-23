-- Rollback for 000160_create_holding_and_liability_history (#1569 T6).
-- Both tables are pure history written beside unchanged current state, so dropping them removes
-- the retained past without touching holdings / credit_liabilities / mortgage_liabilities /
-- student_loan_liabilities, which keep their replace semantics either way.

DROP TABLE IF EXISTS liability_statements CASCADE;
DROP TABLE IF EXISTS holding_snapshots CASCADE;
