-- Rollback: remove user_accounts.exclude_investment_sync (#1609 Item 5).
--
-- The column is a read-layer switch only: nothing was deleted, tombstoned or suppressed at
-- ingestion on the way up, so dropping it restores the pre-migration behaviour exactly — every
-- investment_transactions row becomes visible again, which is the same state the flag's own
-- FALSE value produces. The only thing lost is the user's preference itself.
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
