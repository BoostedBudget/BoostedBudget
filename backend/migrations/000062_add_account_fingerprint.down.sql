-- Rollback: #1010 account fingerprint columns and indexes

-- Drop fingerprint indexes
DROP INDEX IF EXISTS unique_persistent_account_id_active;
DROP INDEX IF EXISTS unique_fallback_fingerprint_active;
DROP INDEX IF EXISTS idx_accounts_persistent_id_blind;
DROP INDEX IF EXISTS idx_accounts_fallback_fingerprint_blind;
DROP INDEX IF EXISTS idx_linked_items_status_changed_at;

-- Remove fingerprint columns from accounts
ALTER TABLE accounts DROP COLUMN IF EXISTS persistent_account_id;
ALTER TABLE accounts DROP COLUMN IF EXISTS persistent_account_id_blind_idx;
ALTER TABLE accounts DROP COLUMN IF EXISTS fallback_fingerprint_blind_idx;

-- Remove status_changed_at from linked_items
ALTER TABLE linked_items DROP COLUMN IF EXISTS status_changed_at;

-- Convert any 'expired' accounts to 'replaced' so the old CHECK constraint can be restored
UPDATE accounts SET status = 'replaced' WHERE status = 'expired';

-- Revert accounts status CHECK constraint (remove 'expired')
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_valid_status;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_valid_status
            CHECK (status IN ('active', 'replaced'));
    END IF;
END $$;
