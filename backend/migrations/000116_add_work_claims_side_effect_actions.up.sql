-- #1413: Add side-effect-level work_claims actions (Codex round-3 findings H1/H2).
--
-- Round 3 established that the existing FLOW actions (plaid_disconnect_past_due,
-- past_due_grace_cancel, subscription_cancelled_cleanup, account_deletion) are keyed by
-- WORKFLOW, not by the SHARED SIDE EFFECT they each perform: all four call
-- DisconnectAllUserItems/RemoveAllUserItems for the same user, but hold DIFFERENT claim rows, so
-- they never exclude one another -- grace cancellation can be blocked inside Plaid while a
-- cancellation webhook wins its OWN (different) claim and calls Plaid for the same user
-- concurrently.
--
-- plaid_disconnect_user and paddle_cancel_subscription are OPERATION-keyed, not flow-keyed:
-- "one Plaid disconnect at a time for this user, whoever wants it" / "one Paddle cancel at a
-- time for this user, whoever wants it" -- claimed INSIDE a flow's own claim, immediately around
-- the external call. See plaid_item_cleanup_service.go, deletion_service.go, and
-- past_due_service.go for the enforced call shape, and the #1413 side-effects design doc for the
-- full two-level model.
--
-- Unlike the existing six flow actions (each conceptually "once ever" per user -- a deletion
-- happens once, a retention notice is sent once per retention cycle), a Plaid disconnect or a
-- Paddle cancel can legitimately recur across a user's lifetime (reconnect, then a LATER dunning
-- cycle disconnects again). Callers of these two actions therefore Release (never Complete) once
-- the external call finishes, win or lose, so the claim row is immediately reclaimable for the
-- next legitimate need instead of being permanently blocked by work_claims' "a completed claim is
-- never re-granted" rule.
--
-- Postgres CHECK constraints cannot be altered in place; the existing inline check must be
-- dropped and recreated with the wider action list. Idempotent: only touches the constraint if
-- it doesn't already include the new values, so re-running this migration (or applying it to a
-- database that already has the wider constraint from a previous partial run) is a no-op.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'work_claims_action_check'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'work_claims_action_check'
          AND pg_get_constraintdef(oid) LIKE '%plaid_disconnect_user%'
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
                'paddle_cancel_subscription'
            ));
    END IF;
END $$;

COMMENT ON CONSTRAINT work_claims_action_check ON work_claims IS
    'Fixed set of destructive/side-effect actions work_claims may hold a claim for. Callers use '
    'the Go action constants in internal/repository -- never free-form strings -- so a typo '
    'fails loudly instead of silently creating an unclaimed, unprotected action. Widened by '
    '#1413 (Codex round 3, findings H1/H2) to add the two OPERATION-keyed side-effect actions '
    'alongside the original six FLOW-keyed actions.';
