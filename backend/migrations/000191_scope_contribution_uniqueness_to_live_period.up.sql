-- #1625 follow-up to 000190, found by adversarial review.
--
-- 000190 introduced archived_at as a ledger-period boundary: an archived row is retained and
-- readable but excluded from the progress sums. What it did NOT do is release that row's hold on
-- the transaction-uniqueness slot, and unique_contribution_per_goal_per_transaction is conditional
-- only on `transaction_id IS NOT NULL AND deleted_at IS NULL`.
--
-- The consequence is that a goal returning to the contributions basis cannot re-record a
-- transaction it recorded in a previous period:
--   * InsertManualFromTransaction upserts with `DO UPDATE ... WHERE status = 'proposed'`, which an
--     archived CONFIRMED row fails, so the user is told the contribution already exists.
--   * UpsertProposed's predicate additionally requires managed_by_reconciliation = true, which
--     ArchiveByGoal sets to false — so the reconciler's upsert silently does nothing.
-- In both cases the new period's contribution is never recorded and never counted, which is the
-- opposite of what 000190 set out to fix.
--
-- Scoping uniqueness to the LIVE period is the correct meaning of the constraint: "one
-- contribution per (transaction, goal)" was always about the CURRENT ledger. An archived row
-- belongs to a closed period and must not constrain the next one. Rows from different periods can
-- now coexist for the same transaction, which is exactly the intent — each counts only while its
-- own period is live.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'unique_contribution_per_goal_per_transaction'
          AND indexdef NOT LIKE '%archived_at IS NULL%'
    ) THEN
        DROP INDEX unique_contribution_per_goal_per_transaction;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS unique_contribution_per_goal_per_transaction
    ON savings_goal_contributions (transaction_id, savings_goal_id)
    WHERE transaction_id IS NOT NULL AND deleted_at IS NULL AND archived_at IS NULL;
