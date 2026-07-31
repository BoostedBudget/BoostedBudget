-- #1413 round-10: replace the derived Plaid-disconnect grace-window anchor (retention_started_at)
-- with an explicit marker column.
--
-- Round-7 introduced a grace window before a read-only (inactive/cancelled) user's Plaid items are
-- disconnected (RetentionCleanupCheckWorker.repairPlaidDisconnects), anchored on
-- retention_started_at. Rounds 8 and 9 showed that anchor cannot be made sound: it is written by
-- several independent paths (TrialService.ExpireTrial, the past-due grace-cancel CAS, the
-- subscription-cancellation webhook, RemoveUserTierOverride's comp-lapse re-arm), its clearing on
-- resubscribe (ClearRetentionDates) can fail and be swallowed by a caller that only logs a
-- warning, and it is NEVER cleared when a Plaid disconnect actually completes (it has to keep
-- counting toward the 6-month retention/deletion clock regardless of Plaid state) — so a stale
-- value can silently persist across a resubscribe-then-recancel cycle, and the round-8 "implausibly
-- old" guard added to compensate only catches staleness older than the ENTIRE retention period,
-- not a shorter but still-wrong stale value. That is structurally undetectable without a new
-- column: an anchor genuinely fresh and one falsely stale-by-one-cycle are indistinguishable using
-- retention_started_at alone.
--
-- plaid_cleanup_pending_at means exactly "this user became ineligible for Plaid access at this
-- instant, and cleanup has not completed" — NULL means nothing is pending. It is set atomically
-- with the SAME statement that transitions subscription_status into the repair pass's target set
-- (inactive/cancelled) — see ExpireTrialIfNoOverride, CancelIfPastDueWithRetention/
-- CancelIfPastDueOccurrenceWithRetention, and UpdateSubscriptionFromPaddle in
-- profile_repository.go — and cleared the moment the user becomes eligible again (ClearRetentionDates,
-- UpdateSubscriptionFromPaddle, and UpdateUserTier on a fresh comp grant) or once a Plaid-disconnect
-- repair pass actually completes (ClearPlaidCleanupPending). The repair pass
-- (GetUsersNeedingPlaidDisconnectRepair) anchors its grace window on this column alone.
ALTER TABLE users ADD COLUMN IF NOT EXISTS plaid_cleanup_pending_at TIMESTAMPTZ;

COMMENT ON COLUMN users.plaid_cleanup_pending_at IS
    'Set the instant a user transitions into a Plaid-disconnect-repair-eligible status '
    '(inactive/cancelled) with cleanup not yet complete; cleared on resubscribe/comp-grant or once '
    'a repair pass successfully disconnects the user''s Plaid items. NULL means nothing pending. '
    'Issue #1413 round-10 — replaces retention_started_at as the repair pass''s grace-window anchor.';

-- Backfill: without this, every user ALREADY inactive/cancelled at deploy time (this column starts
-- NULL for every existing row) would silently stop being eligible for the Plaid-disconnect repair
-- pass entirely, until some future status transition re-sets the marker — the exact "silently
-- never disconnected" failure mode this whole task exists to close. retention_started_at is the
-- best available proxy for "when did this user become ineligible" for a ONE-TIME backfill (it is
-- what the repair pass has been (unsoundly) anchoring on already), but capped at 48 hours ago —
-- mirroring migration 000105's precedent of capping a backfilled dunning timestamp short of its
-- own grace deadline — so a backfilled user is never immediately past the 72-hour grace window on
-- the very first cron run after this migration; ops have at least one full daily cycle to notice
-- before any backfilled row can be disconnected.
UPDATE users
SET plaid_cleanup_pending_at = GREATEST(
    COALESCE(retention_started_at, updated_at, NOW()),
    NOW() - INTERVAL '48 hours'
)
WHERE subscription_status IN ('inactive', 'cancelled')
  AND plaid_cleanup_pending_at IS NULL
  AND (manual_tier_override IS NULL OR manual_tier_override = '');
