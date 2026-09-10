-- #1631 (M4): Reverse 000196_add_user_accounts_investment_backfill_requested_at.up.sql
--
-- ⚠ Dropping this column discards any backfill obligation currently outstanding. That is a loss of
-- an operational marker, not of financial history: no investment_transactions row is touched, and
-- the owner can re-create the obligation by toggling exclude_investment_sync off and on again.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'investment_backfill_requested_at'
    ) THEN
        ALTER TABLE user_accounts DROP COLUMN investment_backfill_requested_at;
    END IF;
END $$;
