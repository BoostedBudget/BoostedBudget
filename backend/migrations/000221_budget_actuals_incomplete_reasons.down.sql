-- Reverses 000221. Drops the last-publish diagnostic column; no row is deleted
-- and no other column is touched.
ALTER TABLE budgets DROP COLUMN IF EXISTS actuals_incomplete_reasons;