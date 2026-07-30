-- #1413 round-5 gap G5 (PR #1443 gap analysis): migration 000119's COMMENT ON COLUMN
-- users.plaid_cleanup_pending_at states the PRE-round-13 rule -- "cleared on resubscribe/comp-grant"
-- and names "UpdateUserTier on a fresh comp grant" as a clear site. Round 13 (36455e78) reversed
-- exactly that: granting a comp suspends CURRENT eligibility only (evaluated separately, at query
-- time, by GetUsersNeedingPlaidDisconnectRepair's noActiveManualOverridePredicate); it does not end
-- the underlying ineligibility episode, so a comp grant deliberately does NOT clear this column.
-- Migration 000119 already ran in every environment and never re-runs, so until this migration
-- `\d+ users` / col_description() permanently displayed the REVERSED rule round 13 identified as
-- this subsystem's P0 root cause -- anyone inspecting the schema was told the opposite of how the
-- system behaves.
--
-- COMMENT ON COLUMN is naturally idempotent (each run simply overwrites the previous comment); the
-- statement is additionally guarded so it is a no-op rather than an error if the column is ever
-- absent, matching this repo's existing DO $$ ... IF EXISTS ... idempotency style (e.g. migration
-- 000093's column guards).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'users'
          AND column_name = 'plaid_cleanup_pending_at'
    ) THEN
        COMMENT ON COLUMN users.plaid_cleanup_pending_at IS
            'Non-NULL exactly when a raw-status ineligibility episode (subscription_status '
            'inactive/cancelled) has begun and cleanup has not completed; NULL means nothing '
            'pending. Deliberately INDEPENDENT of manual_tier_override: granting a comp suspends '
            'current eligibility only, evaluated separately at query time, and does NOT clear this '
            'column -- round 13 reversed an earlier design that cleared it on comp-grant, the P0 '
            'root cause this subsystem exists to fix. Kept honest by the self-healing backstops '
            'StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers '
            '(profile_repository.go). Issue #1413 round-5 gap G5 -- corrects migration 000119''s '
            'comment, which stated the reversed, pre-round-13 rule.';
    END IF;
END $$;
