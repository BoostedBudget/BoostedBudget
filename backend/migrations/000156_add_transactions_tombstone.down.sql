-- Rollback for 000156: reverse ONLY what the up migration added.
--
-- READ THIS BEFORE ROLLING BACK. Dropping these columns does not just discard provenance; it
-- CORRUPTS THE LEDGER, and saying "less destructive than the pre-000156 hard delete" (as an earlier
-- draft of this comment did) understates it to the point of being misleading.
--
--   1. Every settled pending becomes indistinguishable from a live row. A row removed while
--      pending was replaced by a POSTED TWIN that is also in the table. With deleted_at gone,
--      nothing excludes the pending copy, so both are returned and every total that spans it --
--      cash flow, spending by category, budget spent_amount, the transactions list -- DOUBLE-COUNTS
--      that spend. Retained reversals come back to life the same way.
--   2. Under the post-000156 BINARY, this rollback breaks Plaid sync outright.
--      TransactionRepository.TombstoneByExternalID hard-requires deleted_at/deleted_reason/
--      deleted_by, so every `removed` entry fails, and since #1569 closed residual (4) that failure
--      now ABORTS the sync and holds the cursor. The item stops syncing entirely rather than
--      silently skipping. Roll the application back first, or not at all.
--
-- There is no reversal that preserves the distinction: the information lives only in these columns.
-- If a rollback is genuinely required, dump `SELECT id, deleted_at, deleted_reason, deleted_by FROM
-- transactions WHERE deleted_at IS NOT NULL` first, so the classification can be restored on the
-- way back up.
--
-- Idempotent: IF EXISTS on every statement.

-- ⚠ #1625 — this file now REFUSES rather than only warning.
--
-- Everything above stays true; what changed is that it is enforced. The header told an operator to
-- dump the tombstones first and then let the rollback proceed, which is a control only if someone
-- reads it.
--
-- Reachability was established by EXECUTION. Against a freshly rebuilt `freshcheck` migrated to
-- 192, holding one transaction with deleted_at IS NOT NULL:
--     migrate -path backend/migrations -database $FRESHCHECK goto 155
-- ran 192/d ... 156/d without stopping; no later down refuses on a transaction tombstone.
-- Observed afterwards: zero of deleted_at/deleted_reason/deleted_by left in
-- information_schema.columns, and the tombstoned row still in `transactions`, now
-- indistinguishable from a live one -- the double-count described above, reproduced.
--
-- LOCK before COUNT, same transaction: without it a concurrent sync tombstoning a `removed` entry
-- (ROW EXCLUSIVE) does not conflict with the count's ACCESS SHARE and can land in the window
-- before the DROP COLUMN. Same finding as 000190's.
DO $$
DECLARE
    n INTEGER;
BEGIN
    LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;
    SELECT COUNT(*) INTO n FROM transactions WHERE deleted_at IS NOT NULL;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000156: % transactions row(s) are tombstoned '
            '(deleted_at IS NOT NULL). Dropping deleted_at retains those rows but makes them '
            'indistinguishable from live ones, so every settled pending and retained reversal '
            'is counted twice by cash flow, spending, budgets and the transactions list.', n;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_transactions_tombstoned;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_deleted_reason_valid;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_tombstone_consistency;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_at;
