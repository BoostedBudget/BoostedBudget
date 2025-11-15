-- Migration: Create balance_snapshots table for tracking historical account balances
-- Issue: #353 - Balance Tracking Dashboard
-- Description: Stores daily balance snapshots captured from Plaid webhooks

CREATE TABLE IF NOT EXISTS balance_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_balance DECIMAL(15,2),
    available_balance DECIMAL(15,2),
    currency_code VARCHAR(3) DEFAULT 'USD' NOT NULL,
    snapshot_date DATE NOT NULL,
    source VARCHAR(50) NOT NULL,  -- Values: 'plaid_webhook', 'migration', 'manual'
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Ensure one snapshot per account per day
    CONSTRAINT unique_account_date UNIQUE (account_id, snapshot_date)
);

-- Indexes for fast querying
CREATE INDEX IF NOT EXISTS idx_balance_snapshots_account_id ON balance_snapshots(account_id);
CREATE INDEX IF NOT EXISTS idx_balance_snapshots_user_id ON balance_snapshots(user_id);
CREATE INDEX IF NOT EXISTS idx_balance_snapshots_snapshot_date ON balance_snapshots(snapshot_date DESC);
CREATE INDEX IF NOT EXISTS idx_balance_snapshots_user_date ON balance_snapshots(user_id, snapshot_date DESC);

-- Backfill: Create initial snapshot for existing accounts
-- This gives existing users a baseline to start from
INSERT INTO balance_snapshots (
    account_id,
    user_id,
    current_balance,
    available_balance,
    currency_code,
    snapshot_date,
    source
)
SELECT
    id AS account_id,
    user_id,
    current_balance,
    available_balance,
    currency_code,
    CURRENT_DATE AS snapshot_date,
    'migration' AS source
FROM accounts
WHERE current_balance IS NOT NULL
ON CONFLICT (account_id, snapshot_date) DO NOTHING;

-- Add comment for documentation
COMMENT ON TABLE balance_snapshots IS 'Daily balance snapshots captured from Plaid webhooks for historical balance tracking';
COMMENT ON COLUMN balance_snapshots.snapshot_date IS 'The date this balance represents (daily granularity)';
COMMENT ON COLUMN balance_snapshots.source IS 'Source of the snapshot: plaid_webhook, migration, or manual';
