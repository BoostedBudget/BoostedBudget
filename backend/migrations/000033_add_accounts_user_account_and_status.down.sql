-- Rollback: Remove user_account_id and soft-delete fields from accounts
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This rollback will DELETE:
-- - user_account_id references (breaking stable account links)
-- - status column (all accounts treated as active)
-- - replaced_at timestamps
-- - replaced_by_id chain references
--
-- Recommended: Only rollback in development or with confirmed database backup

-- =============================================================================
-- Step 1: Drop Indexes (Idempotent)
-- =============================================================================

DROP INDEX IF EXISTS idx_accounts_user_account;
DROP INDEX IF EXISTS idx_accounts_status;
DROP INDEX IF EXISTS idx_accounts_user_account_status;
DROP INDEX IF EXISTS idx_accounts_replaced_by;

-- =============================================================================
-- Step 2: Drop Constraints (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_valid_status;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'accounts_replaced_at_consistency'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_replaced_at_consistency;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Drop Columns (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'replaced_by_id'
    ) THEN
        ALTER TABLE accounts DROP COLUMN replaced_by_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'replaced_at'
    ) THEN
        ALTER TABLE accounts DROP COLUMN replaced_at;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'status'
    ) THEN
        ALTER TABLE accounts DROP COLUMN status;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'user_account_id'
    ) THEN
        ALTER TABLE accounts DROP COLUMN user_account_id;
    END IF;
END $$;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
