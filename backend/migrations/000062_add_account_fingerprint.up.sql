-- #1010: Enforce Plaid item uniqueness across all users
--
-- Adds account fingerprint columns to the accounts table for cross-user deduplication.
-- Uses two fingerprints:
--   1. persistent_account_id_blind_idx: Plaid's stable cross-session identifier (preferred)
--   2. fallback_fingerprint_blind_idx: institution_id|mask|type|subtype (when Plaid ID unavailable)
--
-- Also adds status_changed_at to linked_items for reliable 90-day TTL clock on disconnected items.
--
-- Note: UNIQUE indexes are partial (WHERE status = 'active') so replaced/expired accounts
-- don't block new links. The 'expired' status is used for TTL-released fingerprints.

-- =============================================================================
-- Step 1: Add fingerprint columns to accounts (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'persistent_account_id'
    ) THEN
        ALTER TABLE accounts ADD COLUMN persistent_account_id TEXT;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'persistent_account_id_blind_idx'
    ) THEN
        ALTER TABLE accounts ADD COLUMN persistent_account_id_blind_idx TEXT;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'fallback_fingerprint_blind_idx'
    ) THEN
        ALTER TABLE accounts ADD COLUMN fallback_fingerprint_blind_idx TEXT;
    END IF;
END $$;

-- =============================================================================
-- Step 2: Add status_changed_at to linked_items (for reliable TTL clock)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'status_changed_at'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN status_changed_at TIMESTAMPTZ;
    END IF;
END $$;

-- Backfill status_changed_at from updated_at for existing rows
UPDATE linked_items SET status_changed_at = updated_at WHERE status_changed_at IS NULL;

-- =============================================================================
-- Step 3: Expand accounts status CHECK constraint to include 'expired'
-- =============================================================================

DO $$
BEGIN
    -- Drop the old constraint (covers 'active' and 'replaced' only)
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_valid_status;
    END IF;

    -- Add expanded constraint
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_valid_status
            CHECK (status IN ('active', 'replaced', 'expired'));
    END IF;
END $$;

-- =============================================================================
-- Step 4: Create partial UNIQUE indexes for cross-user fingerprint enforcement
-- =============================================================================

-- Primary: persistent_account_id (Plaid's stable cross-session identifier)
-- Only covers active accounts so replaced/expired rows don't block new links
CREATE UNIQUE INDEX IF NOT EXISTS unique_persistent_account_id_active
    ON accounts (persistent_account_id_blind_idx)
    WHERE persistent_account_id_blind_idx IS NOT NULL AND status = 'active';

-- Fallback: institution_id|mask|type|subtype composite
-- Used when Plaid's persistent_account_id is unavailable
CREATE UNIQUE INDEX IF NOT EXISTS unique_fallback_fingerprint_active
    ON accounts (fallback_fingerprint_blind_idx)
    WHERE fallback_fingerprint_blind_idx IS NOT NULL AND status = 'active';

-- =============================================================================
-- Step 5: Performance indexes
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_accounts_persistent_id_blind
    ON accounts (persistent_account_id_blind_idx)
    WHERE persistent_account_id_blind_idx IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_accounts_fallback_fingerprint_blind
    ON accounts (fallback_fingerprint_blind_idx)
    WHERE fallback_fingerprint_blind_idx IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_linked_items_status_changed_at
    ON linked_items (status_changed_at)
    WHERE status_changed_at IS NOT NULL;

-- =============================================================================
-- Step 6: Column comments
-- =============================================================================

COMMENT ON COLUMN accounts.persistent_account_id IS '#1010: Plaid stable cross-session account identifier (encrypted). Stable across different Link sessions for the same physical account.';
COMMENT ON COLUMN accounts.persistent_account_id_blind_idx IS '#1010: Blind index for persistent_account_id. Used in unique constraint to prevent same physical account being linked by different users.';
COMMENT ON COLUMN accounts.fallback_fingerprint_blind_idx IS '#1010: Blind index of institution_id|mask|type|subtype. Fallback fingerprint when Plaid persistent_account_id is unavailable.';
COMMENT ON COLUMN linked_items.status_changed_at IS '#1010: Timestamp when item status last changed. Used for 90-day TTL check on disconnected/revoked items before releasing fingerprints.';
