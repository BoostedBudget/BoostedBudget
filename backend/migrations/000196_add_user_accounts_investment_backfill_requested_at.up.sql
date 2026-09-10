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
