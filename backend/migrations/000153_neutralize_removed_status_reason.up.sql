-- Issue #1569 Task 5, fix round 1: stop account_status_history claiming a CAUSE it cannot know.
--
-- 000152's trigger derives `reason` purely from the status transition, and stamped every
-- active -> removed write 'user_disconnect'. That was true while the user-initiated disconnect
-- (accountService.DisconnectAccount / DisconnectInstitution) was the only writer of that
-- transition. Task 5 added a second one: transactionSyncService.reconcileRemovedAccounts marks an
-- account removed when a bank closes it and Plaid stops returning it. Those rows are not user
-- disconnects, and the trigger has no way to tell the two apart -- a trigger sees a status
-- transition, never an intent.
--
-- So the label is neutralised rather than made smarter: active -> removed now records 'removed',
-- which states exactly what is known (the transition) and claims nothing about why. The CAUSE
-- lives in audit_logs, where the two writers already use distinct actions:
--   account.removed              -- the user asked us to disconnect it
--   account.removed_by_provider  -- Plaid stopped returning it; the bank closed it
--
-- ⚠ Rejected alternative: passing a per-cause label from Go through a session GUC (SET LOCAL +
-- current_setting). Earlier work on this branch established that SET LOCAL through a pooled
-- connection is not reliable here -- the repository's status writers are autocommit `Exec` calls
-- with no application-level transaction, so a SET LOCAL either has no transaction to be local to
-- or lands on a pooled connection that a later, unrelated statement inherits. A silently wrong
-- cause is worse than an honestly absent one.
--
-- Pre-existing rows are deliberately NOT relabelled. Every 'user_disconnect' row in the table
-- today was written (or backfilled) when the user path was the only writer of this transition, so
-- the claim is true for those rows; rewriting them would destroy a true statement to buy
-- vocabulary tidiness. From this migration forward the transition records 'removed', and
-- 'user_disconnect' is a closed historical set.
--
-- Idempotent: CREATE OR REPLACE FUNCTION. The two triggers created by 000152 are untouched and
-- keep pointing at this same function name, so nothing needs to be re-attached.

CREATE OR REPLACE FUNCTION record_account_status_change() RETURNS TRIGGER AS $$
DECLARE
    ts TIMESTAMPTZ;
    derived_reason TEXT;
BEGIN
    ts := clock_timestamp();

    IF TG_OP = 'INSERT' THEN
        derived_reason := 'created';
    ELSIF OLD.status = 'active' AND NEW.status = 'replaced' THEN
        derived_reason := 'relink';
    ELSIF OLD.status = 'active' AND NEW.status = 'expired' THEN
        derived_reason := 'fingerprint_expiry';
    ELSIF OLD.status = 'active' AND NEW.status = 'removed' THEN
        -- Neutral by design: two writers produce this transition for different reasons, and the
        -- trigger can distinguish neither. See this file's header.
        derived_reason := 'removed';
    ELSE
        derived_reason := 'status_change:' || OLD.status || '_to_' || NEW.status;
    END IF;

    -- Close BEFORE open (mechanic #3). No-op on INSERT (nothing to close yet).
    UPDATE account_status_history
        SET valid_to = ts
        WHERE account_id = NEW.id AND valid_to IS NULL;

    INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, reason, provenance)
        VALUES (NEW.id, NEW.user_id, NEW.user_account_id, NEW.status, ts, derived_reason, 'recorded');

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
