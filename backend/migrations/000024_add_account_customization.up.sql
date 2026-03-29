-- Migration: Add Account Customization Fields
-- Issue: #307 - Account Customization (nicknames, colors, icons, sorting, hide/archive)
-- Purpose: Allow users to personalize their linked bank accounts
--
-- Fields Added:
--   - nickname: User-defined display name (overrides bank name)
--   - custom_color: Hex color code for UI theming (#RRGGBB)
--   - custom_icon: Predefined icon identifier
--   - custom_emoji: User-selected emoji for visual identification
--   - display_order: Integer for drag-and-drop sorting
--   - is_hidden: Boolean to hide accounts from default views

-- =============================================================================
-- Step 1: Add Customization Columns to accounts Table (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Add nickname column (user-defined display name)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'nickname'
    ) THEN
        ALTER TABLE accounts ADD COLUMN nickname VARCHAR(255);
    END IF;

    -- Add custom_color column (hex format #RRGGBB)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_color'
    ) THEN
        ALTER TABLE accounts ADD COLUMN custom_color VARCHAR(7);
    END IF;

    -- Add custom_icon column (predefined icon identifiers)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_icon'
    ) THEN
        ALTER TABLE accounts ADD COLUMN custom_icon VARCHAR(50);
    END IF;

    -- Add custom_emoji column (UTF-8 emoji)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_emoji'
    ) THEN
        ALTER TABLE accounts ADD COLUMN custom_emoji VARCHAR(10);
    END IF;

    -- Add display_order column (for sorting)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'display_order'
    ) THEN
        ALTER TABLE accounts ADD COLUMN display_order INTEGER;
    END IF;

    -- Add is_hidden column (for hide/archive)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'is_hidden'
    ) THEN
        ALTER TABLE accounts ADD COLUMN is_hidden BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

-- =============================================================================
-- Step 2: Add Constraints (Idempotent)
-- =============================================================================

-- Add check constraint for hex color format
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'valid_hex_color'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT valid_hex_color
            CHECK (custom_color IS NULL OR custom_color ~ '^#[0-9A-Fa-f]{6}$');
    END IF;
END $$;

-- Add check constraint for allowed icons
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'valid_custom_icon'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT valid_custom_icon
            CHECK (custom_icon IS NULL OR custom_icon IN (
                'wallet', 'piggy-bank', 'credit-card', 'bank', 'building',
                'briefcase', 'dollar', 'euro', 'pound', 'yen',
                'chart', 'trending-up', 'trending-down', 'lock', 'unlock',
                'star', 'heart', 'home', 'car', 'plane',
                'shopping-cart', 'gift', 'coffee', 'utensils', 'graduation-cap'
            ));
    END IF;
END $$;

-- =============================================================================
-- Step 3: Create Indexes for Performance
-- =============================================================================

-- Index for filtering hidden accounts efficiently
CREATE INDEX IF NOT EXISTS idx_accounts_is_hidden
    ON accounts(user_id, is_hidden);

-- Index for sorting by display_order
CREATE INDEX IF NOT EXISTS idx_accounts_display_order
    ON accounts(user_id, display_order NULLS LAST);

-- =============================================================================
-- Step 4: Add Column Comments for Documentation
-- =============================================================================

COMMENT ON COLUMN accounts.nickname IS 'User-defined display name. Overrides bank name in UI when set. Max 255 chars.';
COMMENT ON COLUMN accounts.custom_color IS 'Hex color code for UI theming (format: #RRGGBB). Example: #3B82F6 for blue.';
COMMENT ON COLUMN accounts.custom_icon IS 'Predefined icon identifier from allowed set. Used for visual identification.';
COMMENT ON COLUMN accounts.custom_emoji IS 'User-selected emoji for visual identification. Stored as UTF-8 (max 10 chars for compound emojis).';
COMMENT ON COLUMN accounts.display_order IS 'Integer for drag-and-drop sorting. NULL values sort last. Lower numbers appear first.';
COMMENT ON COLUMN accounts.is_hidden IS 'When TRUE, account is hidden from default views. Used for closed/archived accounts.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
