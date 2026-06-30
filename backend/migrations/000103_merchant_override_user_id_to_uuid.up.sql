-- Issue #1348 (PASS 4): Permanently fix the root cause of recurring uuid/text operator
-- conflicts. merchant_name_overrides.user_id was created as TEXT in migration 000101
-- (the only user_id TEXT column in the schema; all others are UUID). Under PostgreSQL's
-- extended protocol, a shared $1 compared against both a TEXT column and a UUID column
-- in one prepared statement triggers SQLSTATE 42883 ("operator does not exist:
-- uuid = text") at parse time, regardless of row presence. This migration aligns the
-- column with the rest of the schema (UUID) and eliminates the class permanently.
--
-- Safety: the prod table is empty at time of migration (first production deploy is
-- pending). ALTER COLUMN TYPE with USING user_id::uuid rebuilds dependent indexes and
-- the unique constraint atomically.
-- squawk:ignore-next-statement prefer-text-field
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE  table_name  = 'merchant_name_overrides'
          AND  column_name = 'user_id'
          AND  data_type   = 'text'
    ) THEN
        ALTER TABLE merchant_name_overrides
            ALTER COLUMN user_id TYPE uuid USING user_id::uuid;
    END IF;
END $$;
