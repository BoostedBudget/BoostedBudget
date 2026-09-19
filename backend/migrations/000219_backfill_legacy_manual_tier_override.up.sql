-- 000219_backfill_legacy_manual_tier_override.up.sql
--
-- One-time repair for #1652. RevenueCat is the sole access authority, and the legacy
-- manual_tier_override/manual_tier_ends_at columns are no longer written by any production path:
-- the admin comp path records a revenuecat_operations intent instead (admin_subscriptions.go's
-- UpdateUserTier delegates to RequestRevenueCatGrant when revenueCat != nil). Rows written before
-- the cutover can still carry a non-NULL override, and a stale override is not inert —
-- noManualOverrideAtAllPredicate (profile_repository.go) excludes a user from the retention-notice/
-- hard-delete queries for as long as ANY override survives on their row, live or lapsed. A user
-- RevenueCat has confirmed inactive but whose legacy override was never cleared is therefore stuck
-- out of the retention pipeline indefinitely.
--
-- "RevenueCat confirms inactive" is read from RC's own stored projection, NOT from the legacy
-- column: the customer has a revenuecat_customers row that has actually been refreshed
-- (access_updated_at IS NOT NULL) and whose stored grants contain no currently-valid grant (every
-- grant is expired, or there are none). A user with a live grant is left untouched — RC says they
-- still have access. The 15-minute freshness window ResolveAccess applies to LIVE decisions is
-- deliberately not applied here: this is a one-time repair, not a live access decision, and the
-- stored grants are the evidence of the last confirmed read.
--
-- clock_timestamp() (not NOW()) is used for the expiry comparison so the repair is not backdated by
-- however long the migration transaction took to reach this statement (db-migration-guide §
-- Grace-Window Timestamps).
--
-- Idempotent: re-running matches nothing once the overrides are NULL.
UPDATE users u
SET manual_tier_override = NULL,
    manual_tier_ends_at = NULL
WHERE u.manual_tier_override IS NOT NULL
  AND u.is_deleted = FALSE
  AND EXISTS (
      SELECT 1
      FROM revenuecat_customers rc
      WHERE rc.user_id = u.id
        AND rc.access_updated_at IS NOT NULL
        AND NOT EXISTS (
            SELECT 1
            FROM jsonb_array_elements(rc.grants) AS g
            WHERE g->>'expires_at' IS NULL
               OR (g->>'expires_at')::timestamptz > clock_timestamp()
        )
  );