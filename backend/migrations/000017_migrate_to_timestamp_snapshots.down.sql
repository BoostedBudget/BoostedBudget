-- ============================================================================
-- Rollback Migration 000017: Revert to DATE-based snapshots
-- ============================================================================
-- WARNING: This rollback LOSES sub-day precision!
-- If multiple snapshots per day exist, only keeps most recent per day
-- ============================================================================
--
-- ⚠⚠ #1569 round 12 task 4 (Codex-cited example): "CRITICAL: destructive if high-frequency
-- snapshots exist" was a comment, not a control -- a rollback is run from a CLI that prints
-- nothing from SQL comments. The guard below REFUSES rather than silently truncating sub-day
-- history when the delete below would actually remove a row. Conditioned on "would delete
-- something" (not on emptiness) for the same reason 000150/000151/000180's guards are: the
-- rollback suite runs `down -all` against a clean/single-snapshot-per-day database, so this is
-- silent there and loud exactly where it matters.

-- Step 1: Drop new indexes
DROP INDEX IF EXISTS idx_balance_snapshots_snapshot_at;
DROP INDEX IF EXISTS idx_balance_snapshots_user_at;
DROP INDEX IF EXISTS idx_balance_snapshots_date_trunc;
DROP INDEX IF EXISTS idx_balance_snapshots_account_at;
-- ⚠ #1626 Phase 3 Task G: this one was NOT created by 000017 and is dropped here anyway, which is a
-- forward reference from an old rollback to a newer migration (000194) and deserves an explanation.
--
-- Step 3 below retypes snapshot_at from TIMESTAMPTZ to DATE, and Postgres REBUILDS every index on
-- that column as part of the ALTER. uq_balance_snapshots_daily_capture is a functional index on
-- ((snapshot_at AT TIME ZONE 'UTC')::date) — immutable while the column is TIMESTAMPTZ (a
-- timestamptz AT TIME ZONE yields a plain timestamp, and timestamp::date is immutable), and NOT
-- immutable once the column is DATE (date AT TIME ZONE yields a timestamptz, and timestamptz::date
-- depends on the session TimeZone). So the rebuild fails with
--
--     ERROR:  functions in index expression must be marked IMMUTABLE  (SQLSTATE 42P17)
--
-- and this whole rollback aborts on a completely opaque error about an index it never mentions.
--
-- A `down -all` would have dropped that index first (000194's own down does), so this only bites a
-- rollback of 000017 in ISOLATION — which is exactly what
-- TestDownMigrationGuards_RefuseWithRowsProceedWhenEmpty_Integration replays, and which is also what
-- an operator does when they want to undo one migration. IF EXISTS makes it a clean no-op on any
-- database that predates 000194. If a future migration adds another functional index on this
-- column, it belongs on this list too.
DROP INDEX IF EXISTS uq_balance_snapshots_daily_capture;

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM balance_snapshots bs1
    WHERE EXISTS (
        SELECT 1 FROM balance_snapshots bs2
        WHERE bs2.account_id = bs1.account_id
          AND DATE(bs2.snapshot_at) = DATE(bs1.snapshot_at)
          AND bs2.snapshot_at > bs1.snapshot_at
    );
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000017: % balance_snapshots row(s) would be permanently deleted '
            'to collapse multiple same-day snapshots into one (this rollback truncates snapshot_at '
            'to DATE precision). Those intra-day snapshots exist nowhere else. Export them, or '
            'delete the ones you are willing to lose yourself, before retrying.', n;
    END IF;
END $$;

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
