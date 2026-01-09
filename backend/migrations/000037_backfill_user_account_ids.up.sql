-- Migration: Backfill user_account_id on accounts and transactions
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Link existing accounts and transactions to their stable user_account
--
-- This migration depends on 000036_backfill_user_accounts having run first.
-- It sets user_account_id on accounts and transactions based on the matching
-- (user_id, institution_id, mask) key.
--
-- Note: This is a DATA migration. No DDL changes.

-- =============================================================================
-- Step 1: Update accounts with user_account_id
-- =============================================================================

-- Set user_account_id on accounts by matching (user_id, institution_id, mask)
UPDATE accounts a
SET user_account_id = ua.id
FROM user_accounts ua
JOIN linked_items li ON a.linked_item_id = li.id
WHERE a.user_account_id IS NULL
  AND a.user_id = ua.user_id
  AND li.institution_id = ua.institution_id
  AND (
    (a.mask IS NULL AND ua.mask IS NULL)
    OR (a.mask = ua.mask)
  );

-- =============================================================================
-- Step 2: Update transactions with user_account_id
-- =============================================================================

-- Set user_account_id on transactions by matching through their account
UPDATE transactions t
SET user_account_id = a.user_account_id
FROM accounts a
WHERE t.user_account_id IS NULL
  AND t.account_id IS NOT NULL
  AND t.account_id = a.id
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Migration Complete
-- =============================================================================
-- Verify with:
-- SELECT COUNT(*) FROM accounts WHERE user_account_id IS NOT NULL;
-- SELECT COUNT(*) FROM transactions WHERE user_account_id IS NOT NULL;
