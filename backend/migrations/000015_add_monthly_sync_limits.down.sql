-- Rollback: Remove monthly sync limits feature
-- WARNING: This rollback is DESTRUCTIVE with PERMANENT DATA LOSS
--
-- Data Loss:
-- - ALL sync audit history will be PERMANENTLY DELETED
-- - Historical sync tracking data CANNOT be recovered without backup
-- - Monthly sync timestamps will be lost from accounts table
--
-- Recommended: Only rollback in development or with confirmed database backup

-- Step 1: Drop index on accounts table
DROP INDEX IF EXISTS idx_accounts_last_synced_month;

-- Step 2: Drop columns from accounts table (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'first_synced_at'
    ) THEN
        ALTER TABLE accounts DROP COLUMN first_synced_at;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'last_synced_at'
    ) THEN
        ALTER TABLE accounts DROP COLUMN last_synced_at;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'last_synced_month'
    ) THEN
        ALTER TABLE accounts DROP COLUMN last_synced_month;
    END IF;
END $$;

-- Step 3: Drop account_sync_audit table and all its data
-- NOTE: CASCADE will drop all foreign key constraints referencing this table
DROP TABLE IF EXISTS account_sync_audit CASCADE;
