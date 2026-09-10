-- Reverses 000191, restoring 000180's predicate.
--
-- ⚠ This can FAIL on data created while 000191 was applied: if a goal round-tripped and recorded
-- the same transaction in two periods, both rows are live (deleted_at IS NULL) and the narrower
-- index rejects the pair. That is correct behaviour for a down migration — it refuses rather than
-- silently destroying one of the two contributions.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'unique_contribution_per_goal_per_transaction'
          AND indexdef LIKE '%archived_at IS NULL%'
    ) THEN
        DROP INDEX unique_contribution_per_goal_per_transaction;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS unique_contribution_per_goal_per_transaction
    ON savings_goal_contributions (transaction_id, savings_goal_id)
    WHERE transaction_id IS NOT NULL AND deleted_at IS NULL;
