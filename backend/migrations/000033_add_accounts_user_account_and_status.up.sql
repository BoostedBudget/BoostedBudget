-- Migration: Add user_account_id and Soft-Delete to accounts
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Link accounts to stable user_accounts and enable soft-delete
--
-- Changes:
-- 1. user_account_id: FK to user_accounts table (stable reference surviving re-links)
-- 2. status: Soft-delete status ('active' or 'replaced')
-- 3. replaced_at: Timestamp when account was replaced
-- 4. replaced_by_id: Self-reference to the replacement account
--
-- Note: This migration depends on 000031_create_user_accounts_table

-- =============================================================================
-- Step 1: Add Columns (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Add user_account_id FK (links to stable user_accounts)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE accounts ADD COLUMN user_account_id UUID REFERENCES user_accounts(id);
    END IF;

    -- Add status column with default 'active'
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'status'
    ) THEN
        ALTER TABLE accounts ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'active';
    END IF;

    -- Add replaced_at timestamp
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'replaced_at'
    ) THEN
        ALTER TABLE accounts ADD COLUMN replaced_at TIMESTAMPTZ;
    END IF;

    -- Add replaced_by_id self-reference
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'replaced_by_id'
    ) THEN
        ALTER TABLE accounts ADD COLUMN replaced_by_id UUID REFERENCES accounts(id);
    END IF;
END $$;

-- =============================================================================
-- Step 2: Add Constraints (Idempotent)
-- =============================================================================

-- Check constraint for valid status values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_valid_status
            CHECK (status IN ('active', 'replaced'));
    END IF;
END $$;

-- Check constraint: replaced_at must be set when status is 'replaced'
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'accounts_replaced_at_consistency'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_replaced_at_consistency
            CHECK (
                (status = 'replaced' AND replaced_at IS NOT NULL) OR
                (status != 'replaced')
            );
    END IF;
END $$;

-- =============================================================================
-- Step 3: Create Indexes for Performance
-- =============================================================================

-- Index for joining accounts to user_accounts
CREATE INDEX IF NOT EXISTS idx_accounts_user_account
    ON accounts(user_account_id);

-- Index for filtering by status
CREATE INDEX IF NOT EXISTS idx_accounts_status
    ON accounts(user_id, status);

-- Index for finding active accounts by user_account
CREATE INDEX IF NOT EXISTS idx_accounts_user_account_status
    ON accounts(user_account_id, status);

-- Index for following the replacement chain
CREATE INDEX IF NOT EXISTS idx_accounts_replaced_by
    ON accounts(replaced_by_id)
    WHERE replaced_by_id IS NOT NULL;

-- =============================================================================
-- Step 4: Add Column Comments for Documentation
-- =============================================================================

COMMENT ON COLUMN accounts.user_account_id IS 'FK to user_accounts. Stable reference surviving Plaid re-links. Multiple accounts can point to the same user_account.';
COMMENT ON COLUMN accounts.status IS 'Soft-delete status: active (current) or replaced (re-linked). Default: active.';
COMMENT ON COLUMN accounts.replaced_at IS 'Timestamp when this account was replaced by a re-link. NULL if active.';
COMMENT ON COLUMN accounts.replaced_by_id IS 'UUID of the account that replaced this one. Forms a chain for multiple re-links.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
