-- Rollback for 000168: drop the pending-twin pointer.
--
-- LOSSY, and unusually cheap to be lossy about. The column holds nothing a user authored and
-- nothing derived from this system's own state: every value is a blind index of an identifier that
-- arrived in a Plaid payload and can arrive again. Dropping it disables the settlement annotation
-- move for rows stored while the column was absent -- their pointers cannot be recovered, because
-- Plaid does not re-deliver a transaction the stored cursor has moved past -- but it destroys no
-- record of anything that happened. The annotations themselves are untouched: the move COPIES, so
-- every annotation it has ever moved still exists on the tombstoned pending row it came from.
--
-- So this rollback does NOT refuse the way 000167's does. That one had to choose between two lies
-- about a user's intent; this one is dropping a cache of a provider identifier.
--
-- Idempotent: DROP INDEX / DROP COLUMN IF EXISTS. The index is dropped first and explicitly rather
-- than left to the column drop's implicit dependency removal, so that a database where the column
-- was already dropped by hand still loses a stray index.

DROP INDEX IF EXISTS idx_transactions_pending_twin;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS pending_transaction_id_blind_idx;
