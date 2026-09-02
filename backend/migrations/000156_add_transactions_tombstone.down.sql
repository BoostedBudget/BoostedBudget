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

DROP INDEX IF EXISTS idx_transactions_tombstoned;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_deleted_reason_valid;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_tombstone_consistency;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_at;
