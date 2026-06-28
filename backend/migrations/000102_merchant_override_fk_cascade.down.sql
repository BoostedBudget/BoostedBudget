-- Rollback 000102: Revert merged_into_merchant_id FK back to ON DELETE SET NULL.

DO $$
BEGIN
    -- Drop the CASCADE FK if it exists.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            DROP CONSTRAINT merchant_name_overrides_merged_into_cascade_fkey;
    END IF;

    -- Restore the original SET NULL FK.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_merchant_id_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            ADD CONSTRAINT merchant_name_overrides_merged_into_merchant_id_fkey
            FOREIGN KEY (merged_into_merchant_id)
            REFERENCES merchants(id)
            ON DELETE SET NULL;
    END IF;
END $$;
