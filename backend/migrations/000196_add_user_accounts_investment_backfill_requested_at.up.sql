-- #1631 (M4): the durable record of an owed investment-transaction backfill.
--
-- WHY THIS COLUMN EXISTS. As of #1631, user_accounts.exclude_investment_sync is no longer a read
-- filter only: the investment sync stops PERSISTING investment_transactions for an excluded chain.
-- That makes clearing the flag a request for data the database does not have, so the sync owes the
-- chain one full-lookback re-fetch. The obligation has to survive a crash between the flag write
-- and the re-fetch, or it is lost forever: the flag transition is true -> false, and a retry of a
-- write that already landed is false -> false, which no transition-triggered mechanism can see a
-- second time.
--
-- WHY IT IS A COLUMN AND NOT A QUEUED JOB. UserAccountService holds a repository and nothing else,
-- and userAccountRepository.Update writes through r.db directly -- there is no pgx.Tx for River's
-- EnqueueTx to join, so a best-effort enqueue alongside the flag write can succeed at the write
-- and fail at the enqueue, silently dropping the obligation. Setting this column inside the SAME
-- UPDATE statement that clears the flag is atomic by definition: either both happen or neither
-- does. It is also self-healing across crashes and inspectable in the database, which a lost job
-- is not.
--
-- LIFECYCLE. Written by userAccountRepository.Update, in the same statement, only on a
-- true -> false transition of exclude_investment_sync (the CASE reads the pre-UPDATE value).
-- Consumed by investmentSyncService.SyncInvestmentTransactions, which widens its start date to the
-- full lookback when any chain on the item carries a marker, and clears the marker only after a
-- run that stored everything Plaid returned. NULL is the steady state and means "nothing owed".
--
-- ⚠ It is deliberately NOT part of the API surface (domain.UserAccount does not project it), so
-- this migration drifts no OpenAPI schema.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'investment_backfill_requested_at'
    ) THEN
        ALTER TABLE user_accounts ADD COLUMN investment_backfill_requested_at TIMESTAMPTZ;
        COMMENT ON COLUMN user_accounts.investment_backfill_requested_at IS 'When the owner cleared exclude_investment_sync, creating an obligation for the investment sync to re-fetch this chain over the full lookback window (#1631). Set in the same UPDATE statement that clears the flag so the obligation cannot be lost between the two writes; cleared only after a sync run that stored every row Plaid returned. NULL means nothing is owed.';
    END IF;
END $$;

-- =============================================================================
-- Re-issue 000187's COMMENT on exclude_investment_sync, which this branch falsified.
-- =============================================================================
--
-- The deployed comment says "This is a READ FILTER ONLY: no investment_transactions row is
-- deleted, tombstoned or suppressed at ingestion, so clearing the flag makes exactly the same rows
-- reappear." The second half of that is now false: the sync suppresses at ingestion, and clearing
-- the flag brings back only the rows that were stored plus whatever the 2-year backfill can
-- re-fetch.
--
-- ⚠ It has to be corrected HERE and not by editing 000187's own file. 000187 has already run in
-- every environment, so its file is a historical record and no edit to it re-executes anywhere;
-- COMMENT ON COLUMN is live schema metadata that only a new migration can change. Anyone reading
-- the column comment out of a production schema dump would otherwise still be told the flag is
-- fully reversible.
--
-- Unconditional and idempotent by nature: COMMENT ON COLUMN replaces whatever is there, and needs
-- no IF NOT EXISTS. Guarded only against the column being absent, which cannot happen after
-- 000187 but keeps a partial-schema replay from failing.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'exclude_investment_sync'
    ) THEN
        COMMENT ON COLUMN user_accounts.exclude_investment_sync IS
        $COMMENT$When TRUE, this chain's investment activity is BOTH hidden from reads and no longer ingested (#1609 Item 5, amended by #1631). Reads: investment_transactions belonging to this chain are hidden from the unified activity feed (transaction_repository.go GetByUserIDKeysetUnified), the direct endpoint GET /api/v1/investments/investment-transactions (investment_transaction_repository.go GetByUserIDKeyset), and the portfolio income surface. Ingestion: investmentSyncService.SyncInvestmentTransactions does not persist new rows for this chain at all; its non-excluded siblings on the same linked item are unaffected and the item watermark still advances for them. Set by the user via PATCH /api/v1/user-accounts/{id} for a brokerage cash-management account that Plaid reports through BOTH /transactions/sync and /investments/transactions/get, which otherwise shows every movement twice. NOT FULLY REVERSIBLE: no stored row is ever deleted or tombstoned, so rows written before the flag was set do reappear when it is cleared -- but rows never ingested during an exclusion can only be recovered by the backfill that clearing the flag records in investment_backfill_requested_at, which reaches 2 years (Plaid's limit on /investments/transactions/get). A chain excluded for longer permanently loses its earliest excluded period. Authorised by docs/decisions/2026-08-24-financial-history-retained-by-default.md section AMENDMENT (#1631): a fourth category -- never ingesting a row. A row whose user_account_id is NULL (legacy; the column has been nullable since 000071) is attributed through accounts.user_account_id for its account_id, exactly as the chain-expansion reads already do, and stays VISIBLE when that yields no excluded chain -- never dropped for want of an attribution.$COMMENT$;
    END IF;
END $$;
