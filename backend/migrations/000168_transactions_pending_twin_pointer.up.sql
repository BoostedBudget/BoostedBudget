-- Issue #1569 Task 4 (#1578): give a posted transaction a pointer back to the pending one it
-- replaced, so the user's work can follow the charge when it settles.
--
-- THE DEFECT. Plaid delivers a settlement as a `removed` entry for the pending transaction plus a
-- brand-new `added` transaction with a different transaction_id. Migration 000156 stopped the
-- removal from destroying the pending row, but a settled pending renders NOWHERE by design
-- (deleted_reason = 'provider_removed_pending' -- surfacing it would double-count the same spend
-- against its twin). So every category override, note, tag and reviewed flag the user put on a
-- pending charge ended up stranded on an invisible row while the posted twin arrived blank. That
-- happens on essentially every pending transaction anybody annotates.
--
-- WHY A COLUMN AND NOT A HEURISTIC. Plaid supplies the link exactly: Transaction.pending_transaction_id
-- is set on the POSTED transaction and holds the transaction_id of the pending one it replaced
-- (plaid-go v23.0.0, plaid/model_transaction.go:47). Matching instead on (account, amount, date
-- window, merchant) can attach one transaction's category to a DIFFERENT transaction -- a silent,
-- unrecoverable corruption of the user's own data, and strictly worse than the bug it fixes.
--
-- WHY BLIND INDEX ONLY, WITH NO PLAINTEXT AND NO CIPHERTEXT COLUMN. The value is never read back;
-- it is only ever MATCHED against transactions.external_transaction_id_blind_idx, which is derived
-- the same way (crypto.FieldEncryptor.BlindIndex). Storing a second copy of a provider identifier
-- we can never need to display would add PII-at-rest for no reader, so it is not stored. This is
-- the same shape migration 000053 gave external_transaction_id_blind_idx, deliberately: the two
-- columns are compared to each other, so they must be produced by the same function.
--
-- NULLABLE, with no default and no backfill. NULL means "not a settlement, or Plaid did not tell
-- us" -- the overwhelmingly common case, and nothing about it needs distinguishing from "unknown".
-- A backfill is impossible in principle: the pointer only ever arrives in a Plaid payload, and the
-- payloads for already-stored transactions are gone.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS.

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS pending_transaction_id_blind_idx TEXT;

-- The one and only read of this column is the settlement move inside
-- TransactionRepository.TombstoneByExternalID, whose predicate is
-- (user_id, pending_transaction_id_blind_idx) with deleted_at IS NULL. The index mirrors that
-- predicate exactly, and is PARTIAL because the column is NULL on all but the small minority of
-- rows that are settlements -- a partial index keeps it proportional to the rows that can ever
-- match rather than to the transactions table.
--
-- Not CONCURRENTLY: golang-migrate runs each migration file inside one transaction (see
-- .squawk.toml's assume_in_transaction note, verified empirically), and CREATE INDEX CONCURRENTLY
-- cannot run in a transaction block. The same trade-off every other index in this migration series
-- makes; require-concurrent-index-creation is excluded in .squawk.toml for that reason.
CREATE INDEX IF NOT EXISTS idx_transactions_pending_twin
    ON transactions (user_id, pending_transaction_id_blind_idx)
    WHERE pending_transaction_id_blind_idx IS NOT NULL;

COMMENT ON COLUMN transactions.pending_transaction_id_blind_idx IS
$COMMENT$Blind index (crypto.FieldEncryptor.BlindIndex) of Plaid's pending_transaction_id: the provider id of the PENDING transaction this POSTED row replaced when the charge settled. NULL on every row that is not a settlement. Deliberately stores neither plaintext nor ciphertext -- the value is never read back, only matched against another row external_transaction_id_blind_idx, so a second at-rest copy of a provider identifier would add PII with no reader. Its sole consumer is the annotation move folded into TransactionRepository.TombstoneByExternalID (#1578), which copies the user-authored columns from a settling pending row onto its posted twin before the pending row is tombstoned invisible. Never fuzzy-match a settlement on amount/date/merchant instead: that can attach one transaction category to a different transaction.$COMMENT$;
