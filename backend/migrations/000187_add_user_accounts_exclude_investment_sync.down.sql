-- Rollback: remove user_accounts.exclude_investment_sync (#1609 Item 5).
--
-- ⚠ CORRECTED BY #1631 (M4). This rollback DOES lose data, and the previous description of it was
-- operationally wrong. It read: "The column is a read-layer switch only: nothing was deleted,
-- tombstoned or suppressed at ingestion on the way up, so dropping it restores the pre-migration
-- behaviour exactly… The only thing lost is the user's preference itself." That was true when
-- written and is false now.
--
-- WHAT ACTUALLY HAPPENS NOW. Since #1631 the investment sync declines to PERSIST
-- investment_transactions for a chain while this flag is TRUE. So:
--
--   * Every investment_transactions row that IS stored does become visible again, exactly as
--     before — nothing was ever deleted or tombstoned, and that part of the old text still holds.
--   * Rows the sync never ingested during an exclusion DO NOT come back. Dropping this column
--     destroys the flag, and with it the only record of which chains were excluded — so nothing is
--     left that could even ask for the missing period. Plaid serves only the last 2 years, so past
--     that horizon the data is unrecoverable by any means.
--   * ⚠ Run 000196's down FIRST if you are rolling back that far: it drops
--     investment_backfill_requested_at, the outstanding-obligation marker. Dropping THIS column
--     while a marker is still standing leaves an obligation whose reason no longer exists.
--
-- BEFORE RUNNING THIS: capture the flagged chains, or accept the loss knowingly.
--     SELECT id, user_id FROM user_accounts WHERE exclude_investment_sync;
--
-- Idempotent: IF EXISTS, so a re-run over an already-rolled-back schema is a no-op.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'exclude_investment_sync'
    ) THEN
        ALTER TABLE user_accounts DROP COLUMN exclude_investment_sync;
    END IF;
END $$;
