-- Rollback for 000180: drop the savings_goal_contributions tombstone.
--
-- Refuses if any row is actually tombstoned: rolling back would silently turn a retained,
-- invisible row back into a live one with no deleted_at to explain it, which is a bigger
-- correctness problem than leaving the migration applied. Same shape as other reversible-guard
-- downs in this tree (e.g. 000167's refusal to narrow accounts_removed_reason_valid while a row
-- still carries the value it would remove).

DO $$
DECLARE
    n INTEGER;
BEGIN
    SELECT COUNT(*) INTO n FROM savings_goal_contributions WHERE deleted_at IS NOT NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000180: % savings_goal_contributions row(s) are tombstoned '
            '(deleted_at IS NOT NULL). Rolling back would drop the column that explains why they '
            'are hidden, silently making them live again.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_savings_goal_contributions_tombstoned;

DROP INDEX IF EXISTS unique_contribution_per_goal_per_transaction;
CREATE UNIQUE INDEX IF NOT EXISTS unique_contribution_per_goal_per_transaction
    ON savings_goal_contributions (transaction_id, savings_goal_id)
    WHERE transaction_id IS NOT NULL;

ALTER TABLE savings_goal_contributions DROP CONSTRAINT IF EXISTS savings_goal_contributions_deleted_reason_valid;
ALTER TABLE savings_goal_contributions DROP CONSTRAINT IF EXISTS savings_goal_contributions_tombstone_consistency;

ALTER TABLE savings_goal_contributions DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE savings_goal_contributions DROP COLUMN IF EXISTS deleted_at;
