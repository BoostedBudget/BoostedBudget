-- =============================================================================
-- Correct the COMMENT on linked_items.investments_skipped_at_link (#1707)
-- =============================================================================
--
-- Migration 000189 created this column and its comment DELIBERATELY UNWIRED: at that time nothing
-- in the codebase read or wrote the column. The wiring landed later -- the link-time writer under
-- #1613 and the reader under #1707 -- so the 000189 comment became false. This migration replaces
-- the WHOLE comment, not just the stale clause, because a schema comment that contradicts the code
-- is a trap for the next reader.
--
-- ⚠ DELIBERATE DIVERGENCE FROM THE ORIGIN: linked_items.investments_skipped_at_link is NOT in
-- reissuedColumnComments (backend/internal/repository/reissued_column_comment_guard_test.go) and
-- must STAY out. That guard asserts every up-migration copy of a watched column's comment is
-- identical to its origin; 000189 is immutable (the issue forbids editing it), so this corrected
-- text can never match it. Do not "fix" this into a red guard.
--
-- IDEMPOTENT: COMMENT ON COLUMN is a catalog write that replaces the prior text, so re-running it
-- (as a container restart does) is safe and never errors.

COMMENT ON COLUMN linked_items.investments_skipped_at_link IS
$COMMENT$TRUE when Plaid granted no investments product for this Item at link time, so the investment sync was never enqueued for it. Written by LinkedItemRepository.SetInvestmentsSkippedAtLink (recordInvestmentsSkippedAtLink, account_service.go) at link time. Read by JobHealthRepository.GetInvestmentsSkippedAtLinkCount as a direct, uncached aggregate. The COLUMN latches TRUE on every flagged link and is never cleared, so it remains TRUE even for a link that is later superseded (replaced) by a re-link. The READER's scope is narrower and separate: it counts only LIVE links -- status NOT IN ('replaced', 'disconnected') -- so a superseded row is not double-counted. Never backfilled. FALSE means "unknown", not "investments were granted", for any row predating migration 000189.$COMMENT$;