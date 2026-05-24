-- Issue #1163/#1165: Add merchants lookup table; link transactions.merchant_id FK.
CREATE TABLE IF NOT EXISTS merchants (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT merchants_name_unique UNIQUE (name)
);

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS merchant_id UUID;
CREATE INDEX IF NOT EXISTS idx_transactions_merchant_id ON transactions (merchant_id);

-- Backfill: insert distinct non-null merchant names from existing transactions
INSERT INTO merchants (id, name, created_at)
SELECT gen_random_uuid(), merchant_name, NOW()
FROM   transactions
WHERE  merchant_name IS NOT NULL
GROUP  BY merchant_name
ON CONFLICT (name) DO NOTHING;

-- Link existing transactions to their merchant row
UPDATE transactions t
SET    merchant_id = m.id
FROM   merchants m
WHERE  t.merchant_name = m.name
  AND  t.merchant_id IS NULL;

-- Add FK constraint as NOT VALID to avoid full table scan lock
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_transactions_merchant_id'
    ) THEN
        ALTER TABLE transactions
            ADD CONSTRAINT fk_transactions_merchant_id
            FOREIGN KEY (merchant_id) REFERENCES merchants(id)
            NOT VALID;
    END IF;
END $$;

-- Validate in a separate step (only checks existing rows, uses ShareUpdateExclusiveLock)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class r ON c.conrelid = r.oid
        WHERE c.conname = 'fk_transactions_merchant_id'
          AND r.relname = 'transactions'
          AND NOT c.convalidated
    ) THEN
        ALTER TABLE transactions VALIDATE CONSTRAINT fk_transactions_merchant_id;
    END IF;
END $$;
