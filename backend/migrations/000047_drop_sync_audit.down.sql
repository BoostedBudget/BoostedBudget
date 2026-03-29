-- Rollback: Restore sync audit table and columns (reverses migration 000047)
-- Re-creates schema from migration 000015_add_monthly_sync_limits.up.sql

CREATE TABLE IF NOT EXISTS account_sync_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    account_id UUID,
    plaid_account_id VARCHAR(255) NOT NULL,
    sync_month VARCHAR(7) NOT NULL,
    first_sync_at TIMESTAMPTZ NOT NULL,
    last_sync_at TIMESTAMPTZ NOT NULL,
    tier_at_sync VARCHAR(50),
    days_historical INTEGER,
    transaction_count INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, plaid_account_id, sync_month)
);

ALTER TABLE account_sync_audit ADD CONSTRAINT fk_account_sync_audit_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE account_sync_audit VALIDATE CONSTRAINT fk_account_sync_audit_user_id;

CREATE INDEX IF NOT EXISTS idx_sync_audit_user_month ON account_sync_audit(user_id, sync_month);
CREATE INDEX IF NOT EXISTS idx_sync_audit_plaid_account ON account_sync_audit(plaid_account_id, sync_month);
CREATE INDEX IF NOT EXISTS idx_sync_audit_month ON account_sync_audit(sync_month, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sync_audit_tier ON account_sync_audit(tier_at_sync, sync_month);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'accounts' AND column_name = 'first_synced_at') THEN
        ALTER TABLE accounts ADD COLUMN first_synced_at TIMESTAMPTZ;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'accounts' AND column_name = 'last_synced_at') THEN
        ALTER TABLE accounts ADD COLUMN last_synced_at TIMESTAMPTZ;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'accounts' AND column_name = 'last_synced_month') THEN
        ALTER TABLE accounts ADD COLUMN last_synced_month VARCHAR(7);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_accounts_last_synced_month ON accounts(user_id, last_synced_month) WHERE last_synced_month IS NOT NULL;
