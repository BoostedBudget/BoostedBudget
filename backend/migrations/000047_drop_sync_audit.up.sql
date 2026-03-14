-- Issue #839: Remove monthly sync limits feature (dead code from planned free tier)
-- Drops the account_sync_audit table and related columns added in migration 000015

-- Drop index on accounts table
DROP INDEX IF EXISTS idx_accounts_last_synced_month;

-- Drop sync tracking columns from accounts table
ALTER TABLE accounts DROP COLUMN IF EXISTS first_synced_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_synced_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_synced_month;

-- Drop the sync audit table (CASCADE drops all indexes automatically)
DROP TABLE IF EXISTS account_sync_audit CASCADE;
