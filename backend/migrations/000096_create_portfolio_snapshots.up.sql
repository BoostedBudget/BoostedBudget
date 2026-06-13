-- #138: Create portfolio_snapshots table and add keyset-covering index to investment_transactions.
--
-- portfolio_snapshots stores one row per (user, UTC calendar day). Each row is a full-portfolio
-- aggregate computed from the holdings table at write time (aggregate-at-write via
-- INSERT ... SELECT SUM(...) FROM holdings WHERE user_id = $1). Written on every holdings sync
-- (skipped when any account was skipped) and by the daily continuity cron.
--
-- snapshot_date is always a UTC calendar date. Callers must pass DATE(now() AT TIME ZONE 'UTC')
-- to avoid session-timezone drift (mirror of the savings_impact_snapshots UTC-day pattern in
-- migration 000092).
--
-- The idx_inv_txn_user_date_id covering index extends the existing (user_id, date DESC) index to
-- include id, enabling the 3-part keyset predicate (sort_date, source, id) used by the unified
-- transaction feed (GetByUserIDKeysetUnified) without an extra sort on deep pages.

-- =============================================================================
-- Step 1: Create portfolio_snapshots table
-- =============================================================================

CREATE TABLE IF NOT EXISTS portfolio_snapshots (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID            NOT NULL,
    snapshot_date       DATE            NOT NULL,
    total_value         NUMERIC(18, 2)  NOT NULL    DEFAULT 0,
    total_cost_basis    NUMERIC(18, 2)  NOT NULL    DEFAULT 0,
    total_gain_loss     NUMERIC(18, 2)  NOT NULL    DEFAULT 0,
    total_gain_loss_pct NUMERIC(10, 4),                          -- NULL when cost_basis = 0
    created_at          TIMESTAMPTZ     NOT NULL    DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL    DEFAULT now()
);

COMMENT ON TABLE portfolio_snapshots IS
    'Daily aggregate snapshots of a user''s investment portfolio (total across all institutions). '
    'One row per (user_id, snapshot_date) UTC calendar day, enforced by '
    'idx_portfolio_snapshots_user_date. Written by holdings-sync and the daily continuity cron.';

COMMENT ON COLUMN portfolio_snapshots.snapshot_date IS
    'UTC calendar date for this snapshot. Always pass DATE(now() AT TIME ZONE ''UTC'') from application code '
    'to avoid session-timezone drift.';

COMMENT ON COLUMN portfolio_snapshots.total_value IS
    'SUM(holdings.institution_value) for this user at write time (all accounts).';

COMMENT ON COLUMN portfolio_snapshots.total_cost_basis IS
    'SUM(holdings.cost_basis) for holdings where cost_basis IS NOT NULL.';

COMMENT ON COLUMN portfolio_snapshots.total_gain_loss IS
    'total_value - total_cost_basis (zero when no cost_basis data available).';

COMMENT ON COLUMN portfolio_snapshots.total_gain_loss_pct IS
    'total_gain_loss / total_cost_basis * 100. NULL when total_cost_basis = 0 to avoid division-by-zero.';

-- =============================================================================
-- Step 2: Unique index — one snapshot per (user, UTC day)
-- =============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS idx_portfolio_snapshots_user_date
    ON portfolio_snapshots (user_id, snapshot_date);

-- =============================================================================
-- Step 3: Supporting indexes
-- =============================================================================

-- Fast "get latest snapshot before date" (GetLatestBefore)
CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_user_date_desc
    ON portfolio_snapshots (user_id, snapshot_date DESC);

-- =============================================================================
-- Step 4: Foreign key — user_id → users(id) ON DELETE CASCADE
-- =============================================================================

DO $$ BEGIN
    ALTER TABLE portfolio_snapshots
        ADD CONSTRAINT fk_portfolio_snapshots_user_id
        FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE portfolio_snapshots VALIDATE CONSTRAINT fk_portfolio_snapshots_user_id;

-- =============================================================================
-- Step 5: updated_at trigger
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_portfolio_snapshots_updated_at'
    ) THEN
        CREATE TRIGGER update_portfolio_snapshots_updated_at
            BEFORE UPDATE ON portfolio_snapshots
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- Step 6: Extend investment_transactions index for keyset pagination tiebreaker
--
-- The unified transaction feed orders by (sort_date DESC, source, id). The existing
-- idx_inv_txn_user_date covers (user_id, date DESC) but does not include id, forcing
-- an extra sort on the id tiebreaker for deep pages. The new covering index
-- (user_id, date DESC, id) eliminates that sort.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_inv_txn_user_date_id
    ON investment_transactions (user_id, date DESC, id);
