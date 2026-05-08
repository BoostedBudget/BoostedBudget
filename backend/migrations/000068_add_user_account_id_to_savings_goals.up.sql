-- Migration: Add user_account_id to savings_goals
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: Give savings_goals a stable reference to user_accounts that survives
--          Plaid re-links (when accounts.id rotates, user_account_id stays constant).
--
-- Pattern: staged backfill (add nullable → backfill → index → FK NOT VALID → VALIDATE)
-- NOT NULL enforcement is deferred to a follow-up issue once all application writes
-- are updated to supply user_account_id.

-- =============================================================================
-- Step 1: Add nullable user_account_id column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'savings_goals' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE savings_goals
        ADD COLUMN user_account_id UUID REFERENCES user_accounts(id) ON DELETE SET NULL;

        COMMENT ON COLUMN savings_goals.user_account_id IS
            'Stable reference to user_accounts surviving Plaid re-links. '
            'Backfilled from accounts.user_account_id. '
            'NOT NULL enforcement deferred to follow-up issue.';
    END IF;
END $$;

-- =============================================================================
-- Step 2: Backfill user_account_id from the accounts table
-- =============================================================================

UPDATE savings_goals sg
SET user_account_id = a.user_account_id
FROM accounts a
WHERE sg.account_id = a.id
  AND sg.user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 3: Create index for efficient user_account_id queries
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_savings_goals_user_account_id
    ON savings_goals(user_account_id);

-- =============================================================================
-- Step 4: Add FK constraint NOT VALID (avoids full table scan on live rows)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_savings_goals_user_account_id'
    ) THEN
        ALTER TABLE savings_goals
        ADD CONSTRAINT fk_savings_goals_user_account_id
        FOREIGN KEY (user_account_id)
        REFERENCES user_accounts(id)
        NOT VALID;
    END IF;
END $$;

-- =============================================================================
-- Step 5: Validate constraint (checks only existing rows, non-blocking)
-- =============================================================================

ALTER TABLE savings_goals VALIDATE CONSTRAINT fk_savings_goals_user_account_id;
