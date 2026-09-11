-- Rollback #1631 M8a: drop the investment-domain currency columns.
--
-- Dropping these loses the recorded currency of every row synced while they existed, and nothing
-- reconstructs it — a later re-sync only re-states the currency of holdings Plaid still reports.
-- That is accepted for a rollback: without the columns every reader falls back to the
-- pre-#1631 behaviour of treating the whole domain as one implicit currency, which is exactly the
-- state this migration reverses.

ALTER TABLE portfolio_snapshots DROP COLUMN IF EXISTS currency_code;
ALTER TABLE holding_snapshots DROP COLUMN IF EXISTS currency_code;
ALTER TABLE investment_transactions DROP COLUMN IF EXISTS currency_code;
ALTER TABLE holdings DROP COLUMN IF EXISTS currency_code;
ALTER TABLE securities DROP COLUMN IF EXISTS currency_code;
