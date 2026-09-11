-- #1631: Reverse 000200_add_securities_consolidation_identifiers.up.sql
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'securities' AND column_name = 'proxy_security_id'
    ) THEN
        ALTER TABLE securities DROP COLUMN proxy_security_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'securities' AND column_name = 'institution_security_id'
    ) THEN
        ALTER TABLE securities DROP COLUMN institution_security_id;
    END IF;
END $$;
