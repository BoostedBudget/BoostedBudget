-- Migration: Add user_account_id to holdings and investment_transactions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: Give investment tables stable references to user_accounts that survive
--          Plaid re-links (when accounts.id rotates, user_account_id stays constant).
--
-- Pattern: staged backfill (add nullable → backfill → index → FK NOT VALID → VALIDATE)
-- NOT NULL enforcement deferred until application writes are updated.

-- =============================================================================
-- PART 1: holdings
-- =============================================================================

-- Step 1: Add nullable user_account_id column
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'holdings' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE holdings
        ADD COLUMN user_account_id UUID;

        COMMENT ON COLUMN holdings.user_account_id IS
            'Stable reference to user_accounts surviving Plaid re-links. '
            'Backfilled from accounts.user_account_id. '
            'NOT NULL enforcement deferred to follow-up issue.';
    END IF;
END $$;

-- Step 2: Backfill user_account_id from the accounts table
UPDATE holdings h
SET user_account_id = a.user_account_id
FROM accounts a
WHERE h.account_id = a.id
  AND h.user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;

-- Step 3: Create index
CREATE INDEX IF NOT EXISTS idx_holdings_user_account_id
    ON holdings(user_account_id);

-- Step 4: Add FK NOT VALID
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_holdings_user_account_id'
    ) THEN
        ALTER TABLE holdings
        ADD CONSTRAINT fk_holdings_user_account_id
        FOREIGN KEY (user_account_id)
        REFERENCES user_accounts(id)
        ON DELETE SET NULL
        NOT VALID;
    END IF;
END $$;

-- Step 5: Validate constraint
ALTER TABLE holdings VALIDATE CONSTRAINT fk_holdings_user_account_id;

-- =============================================================================
-- PART 2: investment_transactions
-- =============================================================================

-- Step 1: Add nullable user_account_id column
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'investment_transactions' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE investment_transactions
        ADD COLUMN user_account_id UUID;

        COMMENT ON COLUMN investment_transactions.user_account_id IS
            'Stable reference to user_accounts surviving Plaid re-links. '
            'Backfilled from accounts.user_account_id. '
            'NOT NULL enforcement deferred to follow-up issue.';
    END IF;
END $$;

-- Step 2: Backfill user_account_id from the accounts table
UPDATE investment_transactions it
SET user_account_id = a.user_account_id
FROM accounts a
WHERE it.account_id = a.id
  AND it.user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;

-- Step 3: Create index
CREATE INDEX IF NOT EXISTS idx_inv_txn_user_account_id
    ON investment_transactions(user_account_id);

-- Step 4: Add FK NOT VALID
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_inv_txn_user_account_id'
    ) THEN
        ALTER TABLE investment_transactions
        ADD CONSTRAINT fk_inv_txn_user_account_id
        FOREIGN KEY (user_account_id)
        REFERENCES user_accounts(id)
        ON DELETE SET NULL
        NOT VALID;
    END IF;
END $$;

-- Step 5: Validate constraint
ALTER TABLE investment_transactions VALIDATE CONSTRAINT fk_inv_txn_user_account_id;
