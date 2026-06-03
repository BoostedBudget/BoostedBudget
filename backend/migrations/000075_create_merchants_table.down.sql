ALTER TABLE transactions DROP CONSTRAINT IF EXISTS fk_transactions_merchant_id;
ALTER TABLE transactions DROP COLUMN IF EXISTS merchant_id;
DROP TABLE IF EXISTS merchants;
