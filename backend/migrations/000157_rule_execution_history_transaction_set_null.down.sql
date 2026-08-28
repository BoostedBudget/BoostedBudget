-- Rollback for 000157: restore migration 000020's ON DELETE CASCADE and the NOT NULL.
--
-- ⚠ This rollback is DESTRUCTIVE in a way the up migration is not: restoring NOT NULL requires
-- removing the orphaned rows this migration's SET NULL created, because there is no transaction to
-- re-point them at. Those rows are deleted here, which is exactly what CASCADE would have done to
-- them in the first place. Nothing else can be done -- the information they lost is unrecoverable.
--
-- Idempotent: IF EXISTS guards, and the orphan delete is a no-op once there are no NULLs left.
--
-- ⚠⚠ #1569 round 12 task 4: "the information they lost is unrecoverable" was a comment, not a
-- control. Same mechanism as this branch's other guards: REFUSE while there is an orphaned row to
-- lose, rather than silently deleting the audit trail for a purged transaction.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM rule_execution_history WHERE transaction_id IS NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000157: % rule_execution_history row(s) have transaction_id '
            'IS NULL (the transaction they acted on has since been purged, and this migration is '
            'the only reason they survived that). Restoring the NOT NULL constraint requires '
            'deleting them, which is unrecoverable. Export them, or delete them yourself having '
            'decided that is acceptable, before retrying.', n;
    END IF;
END $$;

DELETE FROM rule_execution_history WHERE transaction_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_transaction_id'
          AND confdeltype <> 'c'
    ) THEN
        ALTER TABLE rule_execution_history
            DROP CONSTRAINT fk_rule_execution_history_transaction_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_transaction_id'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT fk_rule_execution_history_transaction_id
            FOREIGN KEY (transaction_id) REFERENCES transactions(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE rule_execution_history
            VALIDATE CONSTRAINT fk_rule_execution_history_transaction_id;
    END IF;
END $$;

ALTER TABLE rule_execution_history ALTER COLUMN transaction_id SET NOT NULL;
