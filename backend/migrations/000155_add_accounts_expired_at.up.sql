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
-- Mirrors accounts_removed_at_consistency (000149) / accounts_replaced_at_consistency (000033) --
-- including the fact that all three are ONE-DIRECTIONAL: status = 'expired' IMPLIES expired_at IS
-- NOT NULL, and nothing more. The converse is deliberately not enforced; Step 3 says why.
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
-- accounts_replaced_at_consistency. The guarantee is one-directional and stops there:
--
--     status = 'expired'  =>  expired_at IS NOT NULL
--
-- ⚠ The converse -- expired_at IS NOT NULL => status = 'expired' -- is NOT enforced, and that is a
-- decision, not an omission (#1569 task 7.9; an earlier draft of this header and of
-- domain.Account.ExpiredAt both claimed a biconditional this CHECK has never expressed).
--
-- status holds ONE value, so a biconditional on each of replaced_at, removed_at and expired_at
-- would make the three columns mutually exclusive: a row could record at most one lifecycle event,
-- ever. Any path that moves an account out of a terminal state -- RestoreProviderRemovedAccount
-- already does, for 'provider_absent' -- would then be FORCED BY THE SCHEMA to erase when the
-- earlier state ended. On an issue whose whole thesis is that history is retained by default,
-- mandating that erasure in DDL is the wrong direction to lock in, and DDL is the hardest place to
-- change one's mind.
--
-- Keeping a stale timestamp off a live row is therefore a Go-level discipline rather than a
-- constraint: accountRepository.updateStatusWithRemovalReason writes NULL for every timestamp it is
-- not stamping, and RestoreProviderRemovedAccount NULLs removed_at on the way back to 'active'.
--
-- Contrast accounts_removed_reason_valid (000154), which DOES constrain its converse. That one is
-- safe precisely because removed_reason is not a timestamp: it has no historical value of its own
-- once the status it explains is gone.
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
'expired' (AccountRepository.ExpireAccountsByFingerprint). Never NULL while status = 'expired' --
accounts_expired_at_consistency requires it. Left NULL by every Go writer that moves the row to
another status, but that direction is a code convention, NOT a constraint: the CHECK is
one-directional on purpose (see the Step 3 header). Distinct
from removed_at (user disconnect / provider absence) and replaced_at (re-link). Pre-existing
'expired' rows (this status predates this column, migration 000062) were backfilled from
updated_at, the same proxy migration 000152 already relies on for this gap -- not a precise
timestamp of the release itself, but the best available one.$COMMENT$;
