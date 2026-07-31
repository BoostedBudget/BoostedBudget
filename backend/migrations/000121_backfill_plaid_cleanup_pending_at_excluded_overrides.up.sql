-- #1413 round-14: backfill plaid_cleanup_pending_at for rows migration 000119 permanently
-- excluded because of its override exclusion — corrected here to match round-13's (36455e78)
-- restated invariant.
--
-- 000119's original backfill WHERE ended:
--   AND (manual_tier_override IS NULL OR manual_tier_override = '')
-- which skipped EVERY row carrying any override value — whether that override was still LIVE at
-- migration time OR had already LAPSED (000119 had no manual_tier_ends_at check at all; this is
-- wider than an earlier Codex review described it — "live override only"). 000120 cannot repair
-- these rows: its own WHERE requires plaid_cleanup_pending_at IS NOT NULL, so it only re-stamps a
-- marker that already exists — it never invents one where 000119 skipped it entirely.
--
-- Round 13 (36455e78) restated the invariant this column maintains (see
-- domain.User.PlaidCleanupPendingAt's doc comment): non-NULL exactly when a raw-status
-- ineligibility episode has begun (subscription_status IN ('inactive','cancelled')) and cleanup
-- has not completed — a durable, OVERRIDE-INDEPENDENT fact. CURRENT eligibility (raw status AND a
-- LIVE override) is evaluated separately, entirely at query time, by
-- GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate
-- (profile_repository.go). Under that rule, every row 000119 skipped for carrying ANY override
-- value (live or lapsed) should have carried a marker all along — round 13 changed the rule but
-- did not migrate the data; this migration is that missing data half.
--
-- This migration therefore deliberately does NOT filter on manual_tier_override at all: a row with
-- a currently-LIVE override is correctly stamped here and then correctly EXEMPTED at query time by
-- noActiveManualOverridePredicate — that separation (durable fact in the column, current
-- eligibility in the query) is precisely round 13's design. Do not reintroduce an override
-- exclusion here; that exclusion is the bug this migration exists to close.
--
-- A live linked item is REQUIRED, not optional: GetUsersNeedingPlaidDisconnectRepair filters
-- candidates through an EXISTS(linked_items ... live) check before a repair pass ever reaches
-- ClearPlaidCleanupPendingIfMatches. Stamping a user with NO linked items at all would leave a
-- marker no repair pass can ever clear — recreating the exact phantom-marker residue 000119/000120
-- already left behind for zero-item users (tracked separately as Issue #1453; this migration must
-- not add to that population). The EXISTS clause below is copied VERBATIM from
-- GetUsersNeedingPlaidDisconnectRepair's own liveness predicate (profile_repository.go) —
-- including the `status IS NULL` arm, which is load-bearing: linked_items.status is nullable
-- (migration 000013), and a bare `NOT IN (...)` evaluates to SQL's UNKNOWN (not TRUE) for a NULL
-- status, silently treating a live NULL-status item as dead. This is copied, not re-derived, so the
-- migration and the query cannot drift out of agreement.
--
-- The stamped value follows 000119's own precedent (which in turn cites migration 000105): floor
-- at NOW() - INTERVAL '48 hours' so a backfilled row can never be immediately past the repair
-- pass's 72-hour grace window on the very first cron run after this migration deploys — ops get at
-- least one full daily cycle to notice before Plaid is ever touched. retention_started_at/
-- updated_at are used as the best available proxy for episode-start, exactly as 000119 used them
-- for its own (analogous) population: existing rows that have NEVER been stamped. This differs
-- deliberately from 000120's fix (re-stamp to a fresh NOW()), which corrects an ALREADY-non-NULL
-- marker inherited from an unsound anchor — a different population. This migration only ever
-- touches rows currently NULL (see WHERE below), so it can never clobber a marker 000120 already
-- corrected.
--
-- Idempotent: WHERE plaid_cleanup_pending_at IS NULL means a second run finds nothing left to
-- stamp — a pure no-op.
UPDATE users
SET plaid_cleanup_pending_at = GREATEST(
        COALESCE(retention_started_at, updated_at, NOW()),
        NOW() - INTERVAL '48 hours'
    ),
    updated_at = NOW()
WHERE subscription_status IN ('inactive', 'cancelled')
  AND plaid_cleanup_pending_at IS NULL
  AND is_deleted = FALSE
  AND deletion_requested_at IS NULL
  AND EXISTS (
      SELECT 1 FROM linked_items
      WHERE linked_items.user_id = users.id
        AND (linked_items.status IS NULL OR linked_items.status NOT IN ('disconnected', 'replaced'))
  );
