-- Issue #1569 Task 7: stop Plaid's `removed` array from hard-deleting user-authored work.
--
-- Before this migration, a transaction Plaid reported as removed was destroyed with
-- `DELETE FROM transactions` (services/transaction_sync_service.go processRemovedTransactions,
-- workers/plaid_sync_provider.go processRemoved). No user action was involved, and the delete took
-- the user's own irreplaceable work with it: category overrides, tags, notes, `reviewed` state --
-- and, via migration 000020's ON DELETE CASCADE, the rule_execution_history audit trail as well.
--
-- These columns turn that destruction into a tombstone. Provenance is recorded the way the purge
-- path records it (a linked_item.history_purged audit row): WHEN (deleted_at), WHY (deleted_reason),
-- WHO (deleted_by).
--
-- The two reasons are not cosmetic; they decide visibility, and the split is possible without a
-- pending_transaction_id (which this codebase does not store) because transactions.pending does:
--
--   provider_removed_pending -- the row was `pending = true` when the provider removed it. This is
--     the ordinary pending -> posted lifecycle: a posted twin has already replaced it. The row is
--     retained for forensics and MUST NEVER be shown anywhere, because showing it would
--     double-count against that twin.
--
--   provider_removed_posted -- the row was `pending = false`. The provider reversed or corrected a
--     settled transaction. The row is retained AND is surfacable as reversed, because a closed
--     month silently changing its totals is precisely the failure #1569 exists to end.
--
-- Reads default-exclude every tombstone regardless of reason; surfacing a reversal is opt-in
-- (domain.TransactionFilters.IncludeReversed). See repository/transaction_repository.go
-- transactionTombstoneCondition and the guard test TestNoTransactionReadForgetsTheTombstoneFilter.
--
-- GDPR: a tombstoned row is still a row. transactions.user_id -> users(id) ON DELETE CASCADE is
-- untouched, so erasure still destroys it. Pinned by
-- TestTombstone_GDPRErasureStillDestroysIt_Integration
-- (repository/transaction_tombstone_integration_test.go).
--
-- Idempotent: IF NOT EXISTS on every column and index, pg_constraint guards on both CHECKs.

-- =============================================================================
-- Step 1: Tombstone columns (idempotent)
-- =============================================================================

ALTER TABLE transactions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

-- =============================================================================
-- Step 2: The three columns move together. A reason or an actor without a timestamp is a row no
-- read filter can classify, and a timestamp without a reason is a row the visibility split cannot
-- resolve -- it would be neither hidden-as-a-settled-pending nor surfaced-as-a-reversal.
-- Mirrors accounts_removed_at_consistency (000149) / accounts_expired_at_consistency (000155).
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'transactions_tombstone_consistency'
    ) THEN
        ALTER TABLE transactions ADD CONSTRAINT transactions_tombstone_consistency
            CHECK (
                (deleted_at IS NULL     AND deleted_reason IS NULL     AND deleted_by IS NULL)
             OR (deleted_at IS NOT NULL AND deleted_reason IS NOT NULL AND deleted_by IS NOT NULL)
            ) NOT VALID;
        ALTER TABLE transactions VALIDATE CONSTRAINT transactions_tombstone_consistency;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Constrain the reason vocabulary. An unrecognised reason would fall through the
-- visibility split into "tombstoned but never surfacable", which is indistinguishable from the
-- silent deletion this task removes.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'transactions_deleted_reason_valid'
    ) THEN
        ALTER TABLE transactions ADD CONSTRAINT transactions_deleted_reason_valid
            CHECK (
                deleted_reason IS NULL
             OR deleted_reason IN ('provider_removed_pending', 'provider_removed_posted')
            ) NOT VALID;
        ALTER TABLE transactions VALIDATE CONSTRAINT transactions_deleted_reason_valid;
    END IF;
END $$;

-- =============================================================================
-- Step 4: Index the tombstones only. Live reads filter `deleted_at IS NULL`, which matches almost
-- every row and is served by the existing (user_id, date) indexes with a cheap recheck; an index
-- on that side would never be chosen. The tombstone side is the rare one, and it is what the
-- reversal surface and any future forensics read.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_transactions_tombstoned
    ON transactions (user_id, deleted_reason, date DESC)
    WHERE deleted_at IS NOT NULL;

COMMENT ON COLUMN transactions.deleted_at IS
$COMMENT$When this transaction was tombstoned. NULL for every live row, and every read in the
codebase default-excludes rows where this is NOT NULL. Set instead of DELETE when a provider
reports a transaction as removed, so that user-authored work (categories, tags, notes, reviewed
state) and the rule_execution_history trail survive. Issue #1569.$COMMENT$;

COMMENT ON COLUMN transactions.deleted_reason IS
$COMMENT$Why the row was tombstoned. 'provider_removed_pending' = removed while pending (the
ordinary pending->posted lifecycle; never show it, its posted twin would be double-counted).
'provider_removed_posted' = removed while posted (a bank reversal or correction; surfacable as
reversed via TransactionFilters.IncludeReversed). Issue #1569.$COMMENT$;

COMMENT ON COLUMN transactions.deleted_by IS
$COMMENT$Who tombstoned the row -- the actor, not a user id. 'plaid_sync' for the transaction sync
service and the (currently unwired) plaid sync provider. Recorded for the same reason the purge
path writes a linked_item.history_purged audit row: a retention decision with no recorded cause is
not auditable. Issue #1569.$COMMENT$;
