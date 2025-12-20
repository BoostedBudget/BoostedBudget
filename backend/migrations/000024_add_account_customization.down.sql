-- Rollback: Remove account customization fields
-- WARNING: This rollback will DELETE all customization data
--
-- Data Loss:
-- - All account nicknames will be PERMANENTLY DELETED
-- - All custom colors, icons, and emojis will be lost
-- - All display order preferences will be lost
-- - All hidden account flags will be lost
--
-- Recommended: Only rollback in development or with confirmed database backup

-- Step 1: Drop indexes (idempotent)
DROP INDEX IF EXISTS idx_accounts_is_hidden;
DROP INDEX IF EXISTS idx_accounts_display_order;

-- Step 2: Drop constraints (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'valid_hex_color'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT valid_hex_color;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'valid_custom_icon'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT valid_custom_icon;
    END IF;
END $$;

-- Step 3: Drop columns (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'nickname'
    ) THEN
        ALTER TABLE accounts DROP COLUMN nickname;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_color'
    ) THEN
        ALTER TABLE accounts DROP COLUMN custom_color;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_icon'
    ) THEN
        ALTER TABLE accounts DROP COLUMN custom_icon;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'custom_emoji'
    ) THEN
        ALTER TABLE accounts DROP COLUMN custom_emoji;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'display_order'
    ) THEN
        ALTER TABLE accounts DROP COLUMN display_order;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'is_hidden'
    ) THEN
        ALTER TABLE accounts DROP COLUMN is_hidden;
    END IF;
END $$;
