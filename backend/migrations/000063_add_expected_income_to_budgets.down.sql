-- Issue #1048: Rollback — remove expected_income column from budgets.
ALTER TABLE budgets DROP COLUMN IF EXISTS expected_income;
