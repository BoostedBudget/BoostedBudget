-- Migration: Add Monthly Transaction Sync Limits
-- Issue: #242 - Prevent account/transaction abuse via monthly sync limits
-- Purpose: Limit how many accounts can sync transaction data per calendar month
--
-- Problem: Free tier users can game the system by repeatedly connecting and deleting
-- bank accounts to bypass the 3-account limit, gaining unlimited access to transaction data.
--
-- Solution:
--   1. Monthly sync quota: Track how many accounts sync transactions per month
--   2. Historical depth limits: Free tier gets 30 days, Premium gets 2 years (via Plaid)
--   3. Track by plaid_account_id (not account_id) to prevent delete-reconnect abuse
--   4. UNIQUE constraint allows re-sync of same account without incrementing counter
--
-- Database Changes:
--   - Create account_sync_audit table for tracking monthly syncs
--   - Add optional sync tracking columns to accounts table
--
-- ROI: Prevents $9,600/year in revenue loss from account cycling abuse

-- =============================================================================
-- Step 1: Create account_sync_audit Table
-- =============================================================================
-- Tracks which accounts have synced transaction data in which month
-- Used to enforce monthly sync limits (e.g., free tier: 3 accounts per month)

CREATE TABLE IF NOT EXISTS account_sync_audit (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- References
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id UUID,  -- Can be NULL after account deletion (audit trail persists)
    plaid_account_id VARCHAR(255) NOT NULL,  -- Immutable tracking identifier

    -- Sync tracking
    sync_month VARCHAR(7) NOT NULL,  -- YYYY-MM format (e.g., '2025-11')
    first_sync_at TIMESTAMPTZ NOT NULL,
    last_sync_at TIMESTAMPTZ NOT NULL,

    -- Context
    tier_at_sync VARCHAR(50),  -- User's tier when synced (free, premium, etc.)
    days_historical INTEGER,  -- How many days back synced (30, 365, 730, etc.)
    transaction_count INTEGER DEFAULT 0,  -- Cumulative transactions synced this month

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints
    -- CRITICAL: Same plaid_account_id can only sync once per month per user
    -- This prevents "delete and reconnect" abuse while allowing re-syncs
    UNIQUE (user_id, plaid_account_id, sync_month)
);

-- =============================================================================
-- Step 2: Create Indexes for Fast Limit Checks
-- =============================================================================

-- Index for counting syncs this month (most common query)
CREATE INDEX IF NOT EXISTS idx_sync_audit_user_month
    ON account_sync_audit(user_id, sync_month);

-- Index for checking if specific account synced this month
CREATE INDEX IF NOT EXISTS idx_sync_audit_plaid_account
    ON account_sync_audit(plaid_account_id, sync_month);

-- Index for admin queries (find accounts synced in a month)
CREATE INDEX IF NOT EXISTS idx_sync_audit_month
    ON account_sync_audit(sync_month, created_at DESC);

-- Index for tier-based analytics
CREATE INDEX IF NOT EXISTS idx_sync_audit_tier
    ON account_sync_audit(tier_at_sync, sync_month);

-- =============================================================================
-- Step 3: Add Optional Tracking Columns to accounts Table
-- =============================================================================
-- These columns are optional for quick display in UI ("Last synced: Nov 3")
-- The source of truth is account_sync_audit table

DO $$
BEGIN
    -- Add first_synced_at timestamp
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'first_synced_at'
    ) THEN
        ALTER TABLE accounts ADD COLUMN first_synced_at TIMESTAMPTZ;
    END IF;

    -- Add last_synced_at timestamp
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'last_synced_at'
    ) THEN
        ALTER TABLE accounts ADD COLUMN last_synced_at TIMESTAMPTZ;
    END IF;

    -- Add last_synced_month for quick filtering
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'accounts' AND column_name = 'last_synced_month'
    ) THEN
        ALTER TABLE accounts ADD COLUMN last_synced_month VARCHAR(7);
    END IF;
END $$;

-- Index for finding accounts synced this month (optional, for UI queries)
CREATE INDEX IF NOT EXISTS idx_accounts_last_synced_month
    ON accounts(user_id, last_synced_month)
    WHERE last_synced_month IS NOT NULL;

-- =============================================================================
-- Step 4: Add Column Comments for Documentation
-- =============================================================================

COMMENT ON TABLE account_sync_audit IS 'Tracks monthly transaction syncs to enforce tier-based sync limits and prevent account cycling abuse';
COMMENT ON COLUMN account_sync_audit.plaid_account_id IS 'Immutable identifier from Plaid. Used to track syncs even after account deletion/reconnection.';
COMMENT ON COLUMN account_sync_audit.sync_month IS 'Calendar month in YYYY-MM format. Counter resets each month.';
COMMENT ON COLUMN account_sync_audit.account_id IS 'Reference to accounts table. Can be NULL after account deletion (audit trail persists).';
COMMENT ON COLUMN account_sync_audit.tier_at_sync IS 'User tier when sync occurred (free, premium, premium_plus). For analytics.';
COMMENT ON COLUMN account_sync_audit.days_historical IS 'How many days of historical transactions were synced (30 for free, 730 for premium).';
COMMENT ON COLUMN account_sync_audit.transaction_count IS 'Cumulative transaction count synced this month for this account. Increments on re-syncs.';

COMMENT ON COLUMN accounts.first_synced_at IS 'Timestamp of first transaction sync (ever). For UI display.';
COMMENT ON COLUMN accounts.last_synced_at IS 'Timestamp of most recent transaction sync. For UI display.';
COMMENT ON COLUMN accounts.last_synced_month IS 'Most recent sync month (YYYY-MM format). For quick filtering in UI.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
-- Next steps:
--   1. Run: atlas migrate lint --dir file://migrations --dev-url "docker://postgres/15/test" --latest 1
--   2. Run: atlas migrate hash --dir file://migrations
--   3. Implement SyncAuditRepository to make tests pass (TDD GREEN phase)
--   4. Test on staging environment
--   5. Monitor accounts_synced_monthly{tier} metric after deployment
