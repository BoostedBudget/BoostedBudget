-- Issue #957: Rollback merchant name handling migration.

DROP TABLE IF EXISTS merchant_name_overrides CASCADE;

DROP INDEX IF EXISTS idx_merchants_normalized_name_trgm;
DROP INDEX IF EXISTS idx_merchants_canonical_merchant_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchants_canonical_merchant_id_fkey'
    ) THEN
        ALTER TABLE merchants DROP CONSTRAINT merchants_canonical_merchant_id_fkey;
    END IF;
END $$;

ALTER TABLE merchants DROP COLUMN IF EXISTS canonical_merchant_id;
ALTER TABLE merchants DROP COLUMN IF EXISTS normalized_name;

-- Note: pg_trgm extension is not dropped here since other features may use it.
