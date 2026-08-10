-- Issue #1453 (PR #1443 follow-up sweep): migration 000122's COMMENT ON COLUMN
-- users.plaid_cleanup_pending_at names only TWO self-healing backstops
-- (StampMissingPlaidCleanupMarkers, ClearCompletedPlaidCleanupMarkers) and states the two-conjunct
-- invariant with no caveat. Since 000122 landed, Issue #1455 added a THIRD self-healing backstop,
-- ClearMarkersOnEligibleStatus (profile_repository.go), closing the invariant's OTHER branch
-- (status not ineligible ⇒ marker must be NULL) -- 000122's comment is now incomplete, not merely
-- dated. Separately, Issue #1453 established that "non-NULL" is not by itself a reliable proxy for
-- "cleanup outstanding": a marker can legitimately be non-NULL with ZERO live linked_items rows
-- (000119/000120 migration residue, or an ordinary Plaid-less cancellation with no linked_items
-- rows at all) -- a case the repair pass and its own backstops both deliberately filter out rather
-- than treat as a violation. 000122's comment states the two-conjunct rule without that caveat,
-- which reads as a stronger guarantee than the column actually provides. Anyone running
-- `\d+ users` still sees the pre-#1453/#1455 picture.
--
-- COMMENT ON COLUMN is naturally idempotent (each run simply overwrites the previous comment); the
-- statement is additionally guarded so it is a no-op rather than an error if the column is ever
-- absent, matching this repo's existing DO $$ ... IF EXISTS ... idempotency style (e.g. migrations
-- 000093, 000122).
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
            'Non-NULL when a raw-status ineligibility episode (subscription_status '
            'inactive/cancelled) has begun and its cleanup has not completed; NULL means no '
            'episode is currently open. CAVEAT (Issue #1453): non-NULL is NOT a reliable proxy for '
            '"cleanup outstanding" in isolation -- a marker can be legitimately non-NULL with ZERO '
            'live linked_items rows (000119/000120 migration residue, or an ordinary Plaid-less '
            'cancellation with no linked_items rows at all), a case the repair pass and its own '
            'backstops deliberately filter out rather than treat as a violation. Deliberately '
            'INDEPENDENT of manual_tier_override: granting a comp suspends current eligibility '
            'only, evaluated separately at query time, and does NOT clear this column -- round 13 '
            'reversed an earlier design that cleared it on comp-grant, the P0 root cause this '
            'subsystem exists to fix. Kept honest by THREE self-healing backstops '
            '(profile_repository.go): StampMissingPlaidCleanupMarkers and '
            'ClearCompletedPlaidCleanupMarkers heal the ineligible-status branch; '
            'ClearMarkersOnEligibleStatus (Issue #1455) heals the other branch (status not '
            'ineligible must NULL the marker). Each heals within at most one repair-pass interval, '
            'not instantaneously. Issue #1453 -- corrects migration 000122''s comment, which '
            'named only the first two backstops and omitted this caveat.';
    END IF;
END $$;
