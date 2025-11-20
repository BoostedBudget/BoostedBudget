-- ============================================================================
-- Migration 000017: Migrate balance_snapshots from DATE to TIMESTAMPTZ
-- ============================================================================
-- Issue #364: Breaking change - Renames snapshot_date to snapshot_at
-- Supports multiple snapshots per account per day for Premium+ high-frequency syncs
-- Existing date-based data converted to timestamp (midnight UTC)
-- ============================================================================

BEGIN;

-- Step 1: Drop old unique constraint (allows multiple snapshots per day)
ALTER TABLE balance_snapshots
DROP CONSTRAINT IF EXISTS unique_account_date;

-- Step 2: Drop old indexes (will recreate on new column)
DROP INDEX IF EXISTS idx_balance_snapshots_snapshot_date;
DROP INDEX IF EXISTS idx_balance_snapshots_user_date;

-- Step 3: Rename column
ALTER TABLE balance_snapshots
RENAME COLUMN snapshot_date TO snapshot_at;

-- Step 4: Change type from DATE to TIMESTAMPTZ
-- Existing dates converted to midnight UTC (2024-11-18 → 2024-11-18 00:00:00+00)
ALTER TABLE balance_snapshots
ALTER COLUMN snapshot_at TYPE TIMESTAMPTZ
USING snapshot_at::timestamp AT TIME ZONE 'UTC';

-- Step 5: Create new indexes optimized for timestamp queries
CREATE INDEX idx_balance_snapshots_snapshot_at
ON balance_snapshots(snapshot_at DESC);

CREATE INDEX idx_balance_snapshots_user_at
ON balance_snapshots(user_id, snapshot_at DESC);

-- Step 6: Create functional index for daily aggregation queries
-- Enables fast GROUP BY date queries
-- Uses timezone-fixed cast to ensure IMMUTABLE expression
CREATE INDEX idx_balance_snapshots_date_trunc
ON balance_snapshots(user_id, ((snapshot_at AT TIME ZONE 'UTC')::date));

-- Step 7: Create index for account-specific deduplication checks
CREATE INDEX idx_balance_snapshots_account_at
ON balance_snapshots(account_id, snapshot_at DESC);

-- Step 8: Add comment
COMMENT ON COLUMN balance_snapshots.snapshot_at IS
'Timestamp when this balance snapshot was captured. Supports multiple snapshots per account per day for Premium+ tier high-frequency syncs (10-minute intervals).';

COMMIT;
