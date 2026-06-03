-- Rollback: remove effective_category_name, effective_pfc_primary, effective_pfc_detailed
-- columns and their associated indexes from the transactions table.
-- Issue #1123

DROP INDEX IF EXISTS idx_transactions_effective_category_name;
DROP INDEX IF EXISTS idx_transactions_effective_pfc_primary;

ALTER TABLE transactions DROP COLUMN IF EXISTS effective_category_name;
ALTER TABLE transactions DROP COLUMN IF EXISTS effective_pfc_detailed;
ALTER TABLE transactions DROP COLUMN IF EXISTS effective_pfc_primary;
