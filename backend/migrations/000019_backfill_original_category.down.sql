-- Rollback: Clear the backfilled original_category values
-- Note: This will clear ALL original_category values, not just the backfilled ones
-- This is acceptable because:
-- 1. Before this migration, original_category was never populated
-- 2. New syncs will repopulate it correctly with the code fix
-- Issue #380: Reset to Original Category button not visible

UPDATE transactions
SET original_category = NULL
WHERE original_category IS NOT NULL;
