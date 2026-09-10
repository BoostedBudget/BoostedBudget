-- #1631 (M4): Reverse 000196_add_user_accounts_investment_backfill_requested_at.up.sql
--
-- ⚠ Dropping this column discards any backfill obligation currently outstanding. That is a loss of
-- an operational marker, not of financial history: no investment_transactions row is touched, and
-- the owner can re-create the obligation by toggling exclude_investment_sync off and on again.
--
-- ⚠ This down deliberately does NOT restore 000187's original COMMENT on exclude_investment_sync.
-- Rolling back this migration removes the obligation MARKER; it does not make the flag a read
-- filter again, because that behaviour lives in investmentSyncService, not in the schema. Putting
-- the old "READ FILTER ONLY … fully reversible" text back would leave the deployed schema asserting
-- something the running code contradicts — a worse state than a comment that mentions a column
-- which no longer exists. Revert the code and 000187 together if that is what you mean to do.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'investment_backfill_requested_at'
    ) THEN
        ALTER TABLE user_accounts DROP COLUMN investment_backfill_requested_at;
    END IF;
END $$;
