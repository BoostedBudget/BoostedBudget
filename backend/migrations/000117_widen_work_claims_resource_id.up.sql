-- #1413 P5: widen work_claims.resource_id from UUID to TEXT so recurring FLOW actions can encode
-- a per-OCCURRENCE discriminator in the claim key, not just who/what.
--
-- Defect this closes: work_claims is UNIQUE (resource_type, resource_id, action), and nothing in
-- the codebase ever deletes or resets a row (verified). Every flow claim previously passed
-- resource_id = bare userID, so the moment a recurring flow (Plaid disconnect on past-due,
-- past-due grace cancel, a retention notice, subscription-cancelled cleanup) called Complete,
-- that (user, action) pair became PERMANENTLY unclaimable for the lifetime of the row. Because
-- ClaimAlreadyCompleted correctly maps to success for callers (#1413 round-3 finding H3), this
-- failure is silent: a user who cancels, resubscribes, and cancels again never gets Plaid
-- disconnected and never gets a retention clock; a user entering a second retention cycle is
-- never warned before hard deletion.
--
-- The fix (see internal/repository/work_claims_occurrence.go, OccurrenceResourceID) builds a
-- composite resource_id -- "{userID}:{occurrence discriminator}" -- for the FIVE recurring flow
-- actions, so a retry of the SAME occurrence still collides (the whole point of the claim) while
-- a genuinely NEW occurrence gets a fresh, unclaimed row. account_deletion (terminal, "once
-- ever") and the two OPERATION-keyed side-effect actions (plaid_disconnect_user,
-- paddle_cancel_subscription -- always Released, never Completed, per #1413 round-3 finding H1)
-- are UNCHANGED: they keep using the bare userID.
--
-- A UUID column cannot hold "{uuid}:{timestamp}" or "{uuid}:{paddle event id}" -- this widens it
-- to TEXT. No FK and no CHECK constraint reference resource_id at all, and the only index
-- touching it is the composite UNIQUE constraint (work_claims_unique_active), which is
-- type-agnostic -- see migration 000114's column comment. Idempotent: only runs the ALTER when
-- the column is still UUID, so re-running this migration (or applying it to a database that
-- already has the widened column from a previous partial run) is a no-op.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'work_claims'
          AND column_name = 'resource_id'
          AND data_type = 'uuid'
    ) THEN
        ALTER TABLE work_claims ALTER COLUMN resource_id TYPE TEXT USING resource_id::TEXT;
    END IF;
END $$;

COMMENT ON COLUMN work_claims.resource_id IS
    'The resource key being claimed. For the five actions that legitimately recur across a '
    'user''s lifetime (plaid_disconnect_past_due, past_due_grace_cancel, '
    'subscription_cancelled_cleanup, retention_notice_30d, retention_notice_7d), this is a '
    'COMPOSITE "{userID}:{occurrence discriminator}" built by the single shared helper '
    'internal/repository/work_claims_occurrence.go OccurrenceResourceID -- NOT just the bare '
    'user ID -- so a retry of the SAME occurrence collides (the whole point of the claim) while a '
    'genuinely NEW occurrence (a new dunning cycle, a new retention cycle, a new cancellation '
    'event) gets a fresh, unclaimed row. account_deletion (terminal) and the two OPERATION-keyed '
    'side-effect actions (plaid_disconnect_user, paddle_cancel_subscription) are UNCHANGED: bare '
    'userID, never a composite. Not a foreign key: work_claims must outlive the resource it '
    'references and may in future cover non-user resources. Issue #1413 P5.';
