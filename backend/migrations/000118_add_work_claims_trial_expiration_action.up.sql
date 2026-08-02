-- #1413 round-4 finding H2: add the 'trial_expiration' work_claims action.
--
-- TrialService.ExpireTrial was the ONE destructive flow in this branch with no work_claims flow
-- claim at all -- every sibling flow (account_deletion, past_due_grace_cancel,
-- plaid_disconnect_past_due, subscription_cancelled_cleanup, the two retention-notice actions)
-- already gates its external work (Plaid disconnect, Paddle cancel) behind a durable claim, but
-- trial expiry called PlaidCleanupForTrial.DisconnectAllUserItems unconditionally once its own
-- CAS (ExpireTrialIfNoOverride) succeeded. Adding this action brings ExpireTrial's shape in line
-- with every other destructive flow: Claim -> re-verify -> external work -> Complete/Release.
--
-- Trial expiry is a "once ever" event per user (GrantTrial refuses to grant a second trial once
-- had_trial is set, and nothing ever clears had_trial back to false), so this action is keyed on
-- the bare user ID like account_deletion -- not occurrence-scoped like the five recurring FLOW
-- actions (see work_claims_occurrence.go).
--
-- Postgres CHECK constraints cannot be altered in place; the existing inline check must be
-- dropped and recreated with the wider action list. Idempotent: only touches the constraint if
-- it doesn't already include the new value, so re-running this migration (or applying it to a
-- database that already has the wider constraint from a previous partial run) is a no-op.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'work_claims_action_check'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'work_claims_action_check'
          AND pg_get_constraintdef(oid) LIKE '%trial_expiration%'
    ) THEN
        ALTER TABLE work_claims DROP CONSTRAINT work_claims_action_check;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'work_claims_action_check'
    ) THEN
        ALTER TABLE work_claims
            ADD CONSTRAINT work_claims_action_check CHECK (action IN (
                'plaid_disconnect_past_due',
                'past_due_grace_cancel',
                'account_deletion',
                'retention_notice_30d',
                'retention_notice_7d',
                'subscription_cancelled_cleanup',
                'plaid_disconnect_user',
                'paddle_cancel_subscription',
                'trial_expiration'
            ));
    END IF;
END $$;

COMMENT ON CONSTRAINT work_claims_action_check ON work_claims IS
    'Fixed set of destructive/side-effect actions work_claims may hold a claim for. Callers use '
    'the Go action constants in internal/repository -- never free-form strings -- so a typo '
    'fails loudly instead of silently creating an unclaimed, unprotected action. Widened by '
    '#1413 round-4 finding H2 to add trial_expiration, the last destructive flow that previously '
    'had no work claim at all.';
