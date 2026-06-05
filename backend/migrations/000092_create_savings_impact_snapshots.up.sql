-- #972: Create savings_impact_snapshots table.
--
-- This table stores daily aggregate snapshots of savings transfer automation impact:
--   total_amount     — sum of all completed transfer amounts across all users
--   user_count       — number of distinct users with completed transfers
--   completed_count  — number of completed transfer suggestions included
--
-- One snapshot is recorded per UTC day (enforced by the unique expression index
-- on computed_at::date). The computed_at DESC index supports efficient "get latest"
-- queries without a full table scan.

-- =============================================================================
-- Step 1: Create the table
-- =============================================================================

CREATE TABLE IF NOT EXISTS savings_impact_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    computed_at     TIMESTAMPTZ NOT NULL    DEFAULT now(),
    total_amount    NUMERIC     NOT NULL    DEFAULT 0,
    user_count      INT         NOT NULL    DEFAULT 0,
    completed_count INT         NOT NULL    DEFAULT 0
);

COMMENT ON TABLE savings_impact_snapshots IS
    'Daily aggregate snapshots of savings transfer automation impact (totals across all users). '
    'One row per UTC calendar day, enforced by idx_savings_impact_snapshots_date_unique.';

COMMENT ON COLUMN savings_impact_snapshots.computed_at IS
    'Timestamp when this snapshot was computed. Truncated to UTC day for uniqueness.';

COMMENT ON COLUMN savings_impact_snapshots.total_amount IS
    'Sum of all completed transfer amounts (in user currency) across all users at snapshot time.';

COMMENT ON COLUMN savings_impact_snapshots.user_count IS
    'Number of distinct users with at least one completed transfer suggestion at snapshot time.';

COMMENT ON COLUMN savings_impact_snapshots.completed_count IS
    'Total number of completed transfer suggestions across all users at snapshot time.';

-- =============================================================================
-- Step 2: Index on computed_at DESC for fast "get latest snapshot" queries
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_savings_impact_snapshots_computed_at
    ON savings_impact_snapshots (computed_at DESC);

-- =============================================================================
-- Step 3: Unique expression index on DATE(computed_at) for idempotency
-- Enforces one snapshot per UTC calendar day.
-- Double parens required by PostgreSQL for expression indexes.
-- =============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS idx_savings_impact_snapshots_date_unique
    ON savings_impact_snapshots ((computed_at::date));
