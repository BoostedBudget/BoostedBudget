-- =============================================================================
-- Rollback for 000223_correct_investments_skipped_at_link_comment (#1707)
-- =============================================================================
--
-- This restores the ORIGINAL comment text that migration 000189 wrote, copied VERBATIM out of
-- 000189's up file, so an up -> down -> up cycle restores the schema byte-for-byte.
--
-- ⚠ THE RESTORED TEXT IS KNOWN TO BE STALE, AND THAT IS DELIBERATE. It says "nothing in the
-- codebase reads or writes this column" and "deliberately NOT WIRED" -- both of which are FALSE
-- today: the link-time writer landed under #1613 and the reader
-- (JobHealthRepository.GetInvestmentsSkippedAtLinkCount) landed under #1707. A down migration's
-- job is FAITHFUL REVERSAL, not correction: rolling 000223 back must reproduce exactly the schema
-- 000189 left behind, stale prose included. The migration that CORRECTS the comment is 000223's
-- UP file -- re-apply it to get the accurate text back. Do not "fix" this file to carry the
-- corrected wording; that would make the rollback unfaithful and break the byte-for-byte
-- round-trip.
--
-- IDEMPOTENT: COMMENT ON COLUMN is a catalog write that replaces the prior text, so re-running it
-- (as a container restart does) is safe and never errors.

COMMENT ON COLUMN linked_items.investments_skipped_at_link IS
$COMMENT$RESERVED FOR #1613 P4-T3 -- created by migration 000189 and deliberately NOT WIRED: as of that migration nothing in the codebase reads or writes this column, and it is not backfilled. Intended meaning: TRUE when Plaid granted no investments product for this Item at link time, so the investment sync was never enqueued for it. FALSE is the correct value for every pre-existing row only in the sense that we do not know -- do not read it as evidence that investments WERE granted until P4-T3 lands and starts writing it.$COMMENT$;