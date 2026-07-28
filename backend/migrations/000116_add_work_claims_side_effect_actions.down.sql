-- #1413: Revert -- restore the original six-action work_claims_action_check constraint,
-- dropping the two side-effect actions (plaid_disconnect_user, paddle_cancel_subscription) added
-- by the up migration.
--
-- If any row currently holds one of the two side-effect actions being removed, the ADD
-- CONSTRAINT below fails loudly (existing row violates the narrower check) rather than silently
-- orphaning claim rows the narrower constraint would otherwise reject.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'work_claims_action_check'
    ) THEN
        ALTER TABLE work_claims DROP CONSTRAINT work_claims_action_check;
    END IF;

    ALTER TABLE work_claims
        ADD CONSTRAINT work_claims_action_check CHECK (action IN (
            'plaid_disconnect_past_due',
            'past_due_grace_cancel',
            'account_deletion',
            'retention_notice_30d',
            'retention_notice_7d',
            'subscription_cancelled_cleanup'
        ));
END $$;
