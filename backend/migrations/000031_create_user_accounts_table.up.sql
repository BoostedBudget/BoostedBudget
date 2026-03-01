-- Migration: Create user_accounts Table
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Stable account abstraction that survives Plaid Item re-links
--
-- The user_accounts table provides a stable identifier for real-world bank accounts
-- that persists when Plaid's account_id changes (which happens on Item deletion/re-link).
--
-- Matching strategy: (user_id, institution_id, mask) identifies a unique real-world account.
-- When a user re-links the same bank, we match by institution + mask to link the new
-- Plaid account to the existing user_account, preserving customizations and transaction history.

-- =============================================================================
-- Step 1: Create user_accounts Table (Idempotent)
-- =============================================================================

CREATE TABLE IF NOT EXISTS user_accounts (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign key to users (required)
    user_id UUID NOT NULL,

    -- Matching criteria for re-link detection
    -- These fields identify a unique real-world bank account
    institution_id VARCHAR(255) NOT NULL,    -- Plaid institution ID (stable across re-links)
    mask VARCHAR(10),                         -- Last 4 digits (stable across re-links, may be NULL)
    account_name_hint VARCHAR(255),           -- Original name for fuzzy matching/display

    -- User customization (moved from accounts table for stability)
    -- These survive re-links because user_accounts is stable
    nickname VARCHAR(255),                    -- User-defined display name
    custom_color VARCHAR(7),                  -- Hex color #RRGGBB
    custom_icon VARCHAR(50),                  -- Predefined icon identifier
    custom_emoji VARCHAR(10),                 -- UTF-8 emoji
    display_order INTEGER,                    -- Drag-and-drop sort order
    is_hidden BOOLEAN NOT NULL DEFAULT FALSE, -- Hide from default views

    -- Account type info (from most recent Plaid account)
    primary_type VARCHAR(50),                 -- depository, credit, loan, investment, other
    primary_subtype VARCHAR(50),              -- checking, savings, credit card, etc.

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Step 2: Add Foreign Key Constraints
-- =============================================================================
ALTER TABLE user_accounts ADD CONSTRAINT fk_user_accounts_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE user_accounts VALIDATE CONSTRAINT fk_user_accounts_user_id;

-- =============================================================================
-- Step 3: Add Unique Constraint for Re-Link Matching (Idempotent)
-- =============================================================================

-- Unique constraint: one user_account per (user, institution, mask)
-- This is the key to re-link matching - when a user re-links, we find the existing
-- user_account by institution_id + mask and link the new Plaid account to it.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'unique_user_account_identity'
    ) THEN
        ALTER TABLE user_accounts
            ADD CONSTRAINT unique_user_account_identity
            UNIQUE (user_id, institution_id, mask);
    END IF;
END $$;

-- =============================================================================
-- Step 4: Add Constraints (Idempotent)
-- =============================================================================

-- Add check constraint for hex color format
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_hex_color'
    ) THEN
        ALTER TABLE user_accounts ADD CONSTRAINT user_accounts_valid_hex_color
            CHECK (custom_color IS NULL OR custom_color ~ '^#[0-9A-Fa-f]{6}$');
    END IF;
END $$;

-- Add check constraint for allowed icons (same icons as accounts table)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_custom_icon'
    ) THEN
        ALTER TABLE user_accounts ADD CONSTRAINT user_accounts_valid_custom_icon
            CHECK (custom_icon IS NULL OR custom_icon IN (
                'wallet', 'piggy-bank', 'credit-card', 'bank', 'building',
                'briefcase', 'dollar', 'euro', 'pound', 'yen',
                'chart', 'trending-up', 'trending-down', 'lock', 'unlock',
                'star', 'heart', 'home', 'car', 'plane',
                'shopping-cart', 'gift', 'coffee', 'utensils', 'graduation-cap'
            ));
    END IF;
END $$;

-- Add check constraint for valid account types
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_type'
    ) THEN
        ALTER TABLE user_accounts ADD CONSTRAINT user_accounts_valid_type
            CHECK (primary_type IS NULL OR primary_type IN (
                'depository', 'credit', 'loan', 'investment', 'brokerage', 'other'
            ));
    END IF;
END $$;

-- =============================================================================
-- Step 5: Create Indexes for Performance
-- =============================================================================

-- Index for user lookups (most common query pattern)
CREATE INDEX IF NOT EXISTS idx_user_accounts_user_id
    ON user_accounts(user_id);

-- Index for re-link matching: (user_id, institution_id)
CREATE INDEX IF NOT EXISTS idx_user_accounts_institution
    ON user_accounts(user_id, institution_id);

-- Index for filtering hidden accounts
CREATE INDEX IF NOT EXISTS idx_user_accounts_is_hidden
    ON user_accounts(user_id, is_hidden);

-- Index for sorting by display_order
CREATE INDEX IF NOT EXISTS idx_user_accounts_display_order
    ON user_accounts(user_id, display_order NULLS LAST);

-- =============================================================================
-- Step 6: Add Column Comments for Documentation
-- =============================================================================

COMMENT ON TABLE user_accounts IS 'Stable account abstraction that survives Plaid Item re-links. Groups multiple Plaid account_ids representing the same real-world bank account.';

COMMENT ON COLUMN user_accounts.institution_id IS 'Plaid institution ID. Stable across re-links. Used with mask for re-link matching.';
COMMENT ON COLUMN user_accounts.mask IS 'Last 4 digits of account number. Stable across re-links. May be NULL for some account types.';
COMMENT ON COLUMN user_accounts.account_name_hint IS 'Original account name from first link. Used for display if nickname not set.';
COMMENT ON COLUMN user_accounts.nickname IS 'User-defined display name. Overrides bank name in UI when set.';
COMMENT ON COLUMN user_accounts.custom_color IS 'Hex color code for UI theming (format: #RRGGBB).';
COMMENT ON COLUMN user_accounts.custom_icon IS 'Predefined icon identifier from allowed set.';
COMMENT ON COLUMN user_accounts.custom_emoji IS 'User-selected emoji for visual identification.';
COMMENT ON COLUMN user_accounts.display_order IS 'Integer for drag-and-drop sorting. NULL values sort last.';
COMMENT ON COLUMN user_accounts.is_hidden IS 'When TRUE, account is hidden from default views.';
COMMENT ON COLUMN user_accounts.primary_type IS 'Account type from most recent Plaid link (depository, credit, etc).';
COMMENT ON COLUMN user_accounts.primary_subtype IS 'Account subtype from most recent Plaid link (checking, savings, etc).';

-- =============================================================================
-- Migration Complete
-- =============================================================================
