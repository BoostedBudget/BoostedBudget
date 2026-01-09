-- Rollback: Drop user_accounts table
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This rollback will DELETE all user_accounts data including:
-- - All user account customizations (nicknames, colors, icons, emojis)
-- - All display order preferences
-- - All hidden account flags
-- - The stable account references used for re-link handling
--
-- IMPORTANT: Before running this rollback, ensure:
-- 1. accounts.user_account_id FK is removed (migration 000033 down)
-- 2. transactions.user_account_id FK is removed (migration 000034 down)
--
-- Recommended: Only rollback in development or with confirmed database backup

-- =============================================================================
-- Step 1: Drop Indexes (Idempotent)
-- =============================================================================

DROP INDEX IF EXISTS idx_user_accounts_user_id;
DROP INDEX IF EXISTS idx_user_accounts_institution;
DROP INDEX IF EXISTS idx_user_accounts_is_hidden;
DROP INDEX IF EXISTS idx_user_accounts_display_order;

-- =============================================================================
-- Step 2: Drop Constraints (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Drop unique constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'unique_user_account_identity'
    ) THEN
        ALTER TABLE user_accounts DROP CONSTRAINT unique_user_account_identity;
    END IF;

    -- Drop color check constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_hex_color'
    ) THEN
        ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_valid_hex_color;
    END IF;

    -- Drop icon check constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_custom_icon'
    ) THEN
        ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_valid_custom_icon;
    END IF;

    -- Drop type check constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_type'
    ) THEN
        ALTER TABLE user_accounts DROP CONSTRAINT user_accounts_valid_type;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Drop Table (Idempotent)
-- =============================================================================

DROP TABLE IF EXISTS user_accounts;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
