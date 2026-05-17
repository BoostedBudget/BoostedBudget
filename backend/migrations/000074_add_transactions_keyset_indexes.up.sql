-- Add composite indexes to support keyset (cursor) pagination across all 4 sort keys.
-- Plain CREATE INDEX (not CONCURRENTLY) because golang-migrate wraps .up.sql in a transaction
-- block and CONCURRENTLY is illegal inside a txn.

CREATE INDEX IF NOT EXISTS idx_tx_keyset_date
    ON transactions(user_id, (COALESCE(authorized_date, date)), id);

CREATE INDEX IF NOT EXISTS idx_tx_keyset_merchant
    ON transactions(user_id, (COALESCE(merchant_name, name)), id);

CREATE INDEX IF NOT EXISTS idx_tx_keyset_amount
    ON transactions(user_id, amount, id);

CREATE INDEX IF NOT EXISTS idx_tx_keyset_category
    ON transactions(user_id, (COALESCE(effective_category_name, effective_pfc_primary, pfc_primary)), id);
