-- Backfill original_category for existing transactions
-- This sets original_category to the current category value where it's NULL
-- This allows the "Reset to Original" button to work for existing transactions
-- Issue #380: Reset to Original Category button not visible

-- Only update transactions that:
-- 1. Have a category set (from Plaid)
-- 2. Don't already have original_category set
UPDATE transactions
SET original_category = category
WHERE original_category IS NULL
  AND category IS NOT NULL;
