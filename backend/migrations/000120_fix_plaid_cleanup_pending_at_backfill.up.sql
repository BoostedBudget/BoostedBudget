-- #1413 round-12 Fix 5: correct migration 000119's plaid_cleanup_pending_at backfill.
--
-- 000119's backfill computed:
--   GREATEST(COALESCE(retention_started_at, updated_at, NOW()), NOW() - INTERVAL '48 hours')
-- retention_started_at is exactly the anchor 000119's OWN header comment explains is unsound
-- (written by several independent paths, never cleared once a disconnect completes, so it can
-- silently carry a PRIOR ineligibility episode's age) — using it here could adopt a much-older
-- timestamp than the user's CURRENT episode actually started. The 48-hour cap only bounds the
-- WORST case to "at least 24 hours of grace" (72h grace period minus the 48h cap), not "a full
-- grace window" — an existing user backfilled from a stale, months-old retention_started_at got
-- far less protection than a user whose marker was set by a genuine, fresh transition.
--
-- Fix: re-stamp plaid_cleanup_pending_at to the instant THIS migration runs, for every user
-- CURRENTLY read-only-status, uncomped, with cleanup still outstanding (marker already non-NULL —
-- this migration only corrects an EXISTING pending marker's value, it must never invent one where
-- the invariant says none exists; see domain.User.PlaidCleanupPendingAt's doc comment and Issue
-- #1413 round-12 Fix 4, which closes the identical "resurrecting a marker" hazard in application
-- code). Since this marker gates an irreversible external Plaid disconnect call, erring toward a
-- LATER value is the safe direction (round-12 Fix 5): the worst case this correction produces is an
-- already-eligible user getting one additional full grace window before repairPlaidDisconnects
-- (retention_cleanup_check.go) considers them again — never a premature disconnect.
--
-- Override predicate deliberately mirrors GetUsersNeedingPlaidDisconnectRepair's noActiveManualOverridePredicate
-- (Issue #1413 round-12 Fix 2 — live-only, not "any override, live or lapsed"): a lapsed comp must
-- not exempt a user's marker from this same correction, matching the repair query's own semantics.
--
-- Idempotent: safe to re-run (each run simply re-stamps NOW() again for whatever still matches);
-- golang-migrate only applies this once per environment regardless.
UPDATE users
SET plaid_cleanup_pending_at = NOW(),
    updated_at = NOW()
WHERE subscription_status IN ('inactive', 'cancelled')
  AND is_deleted = FALSE
  AND deletion_requested_at IS NULL
  AND plaid_cleanup_pending_at IS NOT NULL
  AND NOT (
      manual_tier_override IS NOT NULL
      AND manual_tier_override <> ''
      AND (manual_tier_ends_at IS NULL OR manual_tier_ends_at >= NOW())
  );
