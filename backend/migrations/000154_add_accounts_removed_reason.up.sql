-- Issue #1569 Task 5, fix round 2: give the CAUSE of a removal a queryable home on the account row.
--
-- WHY THIS COLUMN EXISTS. Provider-initiated removals are now auto-restored: if an account this
-- product marked removed because Plaid stopped returning it reappears in a later confirmed-complete
-- roster, it goes back to 'active'. That closes the permanence hole (nothing else in this codebase
-- ever writes an account back to 'active'), but it needs one hard guarantee:
--
--   ⚠ A USER-DISCONNECTED ACCOUNT MUST NEVER BE AUTO-RESTORED.
--
-- The user asked for it gone. Resurrecting it because the bank still reports it would invert the
-- whole principle of Issue #1569. So restore must be able to ask, of one account row, "who removed
-- this?" -- and nothing could answer that:
--
--   - account_status_history.reason deliberately cannot (migration 000153): the trigger sees a
--     status transition, never an intent, so it records the neutral 'removed'. Re-specialising it
--     would re-introduce exactly the false-cause defect 000153 removed.
--   - audit_logs can distinguish the two actions, but it is an append-only trail, not queryable
--     current state, and its rows carry a NULL actor by design (see writeAuditLogEntry). Deriving
--     a live authorisation decision from it would be reading a log as a database.
--
-- This column is current state, which is exactly what the restore decision needs. It neither
-- rewrites history nor reopens the label question 000153 settled.
--
-- ⚠ NULL MEANS "NOT PROVIDER-REMOVED", HENCE NOT RESTORABLE. Every 'removed' row that exists
-- before this migration has NULL here, and there is deliberately no backfill: those rows predate
-- the provider path entirely, so they were all user disconnects, and guessing otherwise would risk
-- resurrecting an account a user deliberately disconnected. The restore predicate is a plain
-- equality (`removed_reason = 'provider_absent'`), which is FALSE for NULL in SQL -- so the safe
-- direction is the one the type system gives us for free, not one a future reader must remember.
--
-- Idempotent: IF NOT EXISTS on the column, pg_constraint guard on the CHECK.

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS removed_reason TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_removed_reason_valid'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_removed_reason_valid
            CHECK (
                removed_reason IS NULL
                OR (status = 'removed' AND removed_reason IN ('user_disconnect', 'provider_absent'))
            );
    END IF;
END $$;

COMMENT ON COLUMN accounts.removed_reason IS
$COMMENT$Who removed this account, for rows with status = 'removed': 'user_disconnect' (the user asked, via DisconnectAccount/DisconnectInstitution) or 'provider_absent' (Plaid stopped returning it from a confirmed-complete /accounts/get, i.e. the bank closed it). NULL for every other status, and for 'removed' rows written before migration 000154. ONLY 'provider_absent' is eligible for automatic restore -- NULL is not, deliberately: a user disconnect must never be undone by a sync. The CAUSE lives here rather than in account_status_history.reason, which records the transition only (see migration 000153).$COMMENT$;
