-- Issue #1569 Task 6: give 'expired' the same timestamp discipline 'replaced' and 'removed' already
-- have. Before this migration, 'expired' was the only terminal status with no paired timestamp
-- column — migration 000152's account_status_history backfill had to fall back to accounts.updated_at
-- as a proxy for when an account expired, because nothing more specific existed. That gap is closed
-- here the same way 000033 (replaced_at) and 000149 (removed_at) closed it for their statuses.
--
-- 'expired' predates this branch (migration 000062), so unlike 000149's removed_at -- which had ZERO
-- pre-existing rows to reconcile because 'removed' did not exist yet -- there may already be
-- production rows with status = 'expired' and no expired_at. Step 2 backfills those from updated_at,
-- the same proxy 000152 already uses for this exact gap, before the CHECK in Step 3 is added; skipping
-- the backfill would make the CHECK fail outright on any pre-existing expired row.
--
-- Mirrors accounts_removed_at_consistency (000149) / accounts_replaced_at_consistency (000033):
-- status = 'expired' <=> expired_at IS NOT NULL.
--
-- Idempotent: IF NOT EXISTS on the column, a backfill guarded by `WHERE expired_at IS NULL` (a no-op
-- once applied), and a pg_constraint guard on the CHECK.

-- =============================================================================
-- Step 1: Add expired_at column (Idempotent)
-- =============================================================================

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS expired_at TIMESTAMPTZ;

-- =============================================================================
-- Step 2: Backfill expired_at for pre-existing 'expired' rows, using updated_at as the best
-- available proxy for when the fingerprint-release write happened (same proxy 000152 already uses
-- for these same rows' account_status_history interval).
-- =============================================================================

UPDATE accounts
SET expired_at = updated_at
WHERE status = 'expired' AND expired_at IS NULL;

-- =============================================================================
-- Step 3: Pair 'expired' with expired_at, mirroring accounts_removed_at_consistency /
-- accounts_replaced_at_consistency. Constrains it so expired_at cannot be set on a row that is not
-- 'expired', same discipline as 000154's accounts_removed_reason_valid.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_expired_at_consistency'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_expired_at_consistency
            CHECK (
                (status = 'expired' AND expired_at IS NOT NULL) OR (status <> 'expired')
            );
    END IF;
END $$;

COMMENT ON COLUMN accounts.expired_at IS
$COMMENT$Timestamp when the 90-day fingerprint TTL was released, moving this account to status =
'expired' (AccountRepository.ExpireAccountsByFingerprint). NULL for every other status. Distinct
from removed_at (user disconnect / provider absence) and replaced_at (re-link). Pre-existing
'expired' rows (this status predates this column, migration 000062) were backfilled from
updated_at, the same proxy migration 000152 already relies on for this gap -- not a precise
timestamp of the release itself, but the best available one.$COMMENT$;
