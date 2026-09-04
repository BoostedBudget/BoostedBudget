-- Rollback: Create investment tables
-- Issue #659: Plaid Investments
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH for investment_transactions (#1569 round 12 task 4).
-- holdings and securities are LIVE STATE -- ReplaceForAccount deletes and re-inserts them on every
-- sync, so dropping them destroys nothing a resync cannot reproduce. investment_transactions is
-- different: it is the append-only record of every buy/sell/dividend/fee Plaid ever reported,
-- upserted by plaid_investment_transaction_id, with no deleted_at and no Go delete path at all
-- (#1569 T8). A resync does not replay history Plaid itself has aged out of its own API window, so
-- dropping this table can destroy money history nothing else will ever re-report. Same mechanism
-- as 000150/000151/000180: fail loudly rather than silently destroy, conditioned on rows existing
-- so an empty database (the rollback suite's clean_database) is unaffected.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM investment_transactions;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000051: investment_transactions holds % row(s), and this '
            'rollback DROPs the table. It is the only record of every buy/sell/dividend/fee Plaid '
            'has ever reported for these accounts -- it has no deleted_at and nothing resyncs '
            'history Plaid itself no longer serves. Export it, or empty it deliberately, before '
            'retrying.', n;
    END IF;
END $$;

-- Remove column from linked_items first
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'investments_last_synced_date'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN investments_last_synced_date;
    END IF;
END $$;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS investment_transactions;
DROP TABLE IF EXISTS holdings;
DROP TABLE IF EXISTS securities;
