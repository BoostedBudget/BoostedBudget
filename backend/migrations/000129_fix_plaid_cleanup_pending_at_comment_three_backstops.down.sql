-- Restores migration 000122's COMMENT ON COLUMN text verbatim -- "down" here means reverting the
-- comment text this migration's up.sql corrected, not dropping the comment or the column (the
-- column itself remains owned by migration 000119 / its own down migration). Guarded the same way
-- as up.sql so it is a no-op rather than an error if the column is ever absent.
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
