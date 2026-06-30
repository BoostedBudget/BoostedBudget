-- Rollback: revert merchant_name_overrides.user_id from UUID back to TEXT.
-- Uses USING user_id::text which is safe (every UUID has a valid text representation).
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE  table_name  = 'merchant_name_overrides'
          AND  column_name = 'user_id'
          AND  data_type   = 'uuid'
    ) THEN
        ALTER TABLE merchant_name_overrides
            ALTER COLUMN user_id TYPE text USING user_id::text;
    END IF;
END $$;
