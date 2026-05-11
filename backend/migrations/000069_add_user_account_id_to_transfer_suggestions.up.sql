-- Migration: Add user_account_id to transfer_suggestions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: Give transfer_suggestions stable references to user_accounts that survive
--          Plaid re-links. Both from_account_id and to_account_id may rotate when
--          a user re-links their bank; the new *_user_account_id columns remain stable.
--
-- Pattern: staged backfill (add nullable → backfill → index → FK NOT VALID → VALIDATE)
-- NOT NULL enforcement deferred until application writes are updated.

-- =============================================================================
-- Step 1a: Add nullable from_user_account_id column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'from_user_account_id'
    ) THEN
        ALTER TABLE transfer_suggestions
        ADD COLUMN from_user_account_id UUID;

        COMMENT ON COLUMN transfer_suggestions.from_user_account_id IS
            'Stable reference to the source user_account surviving Plaid re-links. '
            'Backfilled from accounts.user_account_id via from_account_id. '
            'NOT NULL enforcement deferred to follow-up issue.';
    END IF;
END $$;

-- =============================================================================
-- Step 1b: Add nullable to_user_account_id column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'to_user_account_id'
    ) THEN
        ALTER TABLE transfer_suggestions
        ADD COLUMN to_user_account_id UUID;

        COMMENT ON COLUMN transfer_suggestions.to_user_account_id IS
            'Stable reference to the destination user_account surviving Plaid re-links. '
            'Backfilled from accounts.user_account_id via to_account_id. '
            'NOT NULL enforcement deferred to follow-up issue.';
    END IF;
END $$;

-- =============================================================================
-- Step 2: Backfill from_user_account_id from the accounts table
-- =============================================================================

UPDATE transfer_suggestions ts
SET from_user_account_id = a.user_account_id
FROM accounts a
WHERE ts.from_account_id = a.id
  AND ts.from_user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 3: Backfill to_user_account_id from the accounts table
-- =============================================================================

UPDATE transfer_suggestions ts
SET to_user_account_id = a.user_account_id
FROM accounts a
WHERE ts.to_account_id = a.id
  AND ts.to_user_account_id IS NULL
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 4: Indexes for efficient user_account_id queries
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_transfer_sugg_from_user_account_id
    ON transfer_suggestions(from_user_account_id);

CREATE INDEX IF NOT EXISTS idx_transfer_sugg_to_user_account_id
    ON transfer_suggestions(to_user_account_id);

-- =============================================================================
-- Step 5a: Add from_user_account_id FK NOT VALID
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_transfer_sugg_from_user_account_id'
    ) THEN
        ALTER TABLE transfer_suggestions
        ADD CONSTRAINT fk_transfer_sugg_from_user_account_id
        FOREIGN KEY (from_user_account_id)
        REFERENCES user_accounts(id)
        ON DELETE SET NULL
        NOT VALID;
    END IF;
END $$;

-- =============================================================================
-- Step 5b: Add to_user_account_id FK NOT VALID
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_transfer_sugg_to_user_account_id'
    ) THEN
        ALTER TABLE transfer_suggestions
        ADD CONSTRAINT fk_transfer_sugg_to_user_account_id
        FOREIGN KEY (to_user_account_id)
        REFERENCES user_accounts(id)
        ON DELETE SET NULL
        NOT VALID;
    END IF;
END $$;

-- =============================================================================
-- Step 6: Validate constraints
-- =============================================================================

ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_sugg_from_user_account_id;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_sugg_to_user_account_id;
