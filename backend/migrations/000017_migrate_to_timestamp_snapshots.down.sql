-- ============================================================================
-- Rollback Migration 000017: Revert to DATE-based snapshots
-- ============================================================================
-- WARNING: This rollback LOSES sub-day precision!
-- If multiple snapshots per day exist, only keeps most recent per day
-- ============================================================================

-- Step 1: Drop new indexes
DROP INDEX IF EXISTS idx_balance_snapshots_snapshot_at;
DROP INDEX IF EXISTS idx_balance_snapshots_user_at;
DROP INDEX IF EXISTS idx_balance_snapshots_date_trunc;
DROP INDEX IF EXISTS idx_balance_snapshots_account_at;

-- Step 2: Delete duplicate snapshots (keep only latest per account per day)
-- CRITICAL: This is DESTRUCTIVE if high-frequency snapshots exist
DELETE FROM balance_snapshots bs1
WHERE EXISTS (
    SELECT 1 FROM balance_snapshots bs2
    WHERE bs2.account_id = bs1.account_id
      AND DATE(bs2.snapshot_at) = DATE(bs1.snapshot_at)
      AND bs2.snapshot_at > bs1.snapshot_at
);

-- Step 3: Convert TIMESTAMPTZ back to DATE (truncates to day)
ALTER TABLE balance_snapshots
ALTER COLUMN snapshot_at TYPE DATE
USING DATE(snapshot_at);

-- Step 4: Rename column back
ALTER TABLE balance_snapshots
RENAME COLUMN snapshot_at TO snapshot_date;

-- Step 5: Restore old unique constraint
ALTER TABLE balance_snapshots
ADD CONSTRAINT unique_account_date UNIQUE (account_id, snapshot_date);

-- Step 6: Restore old indexes
CREATE INDEX idx_balance_snapshots_snapshot_date
ON balance_snapshots(snapshot_date DESC);

CREATE INDEX idx_balance_snapshots_user_date
ON balance_snapshots(user_id, snapshot_date DESC);
