-- Rollback: Create liability tables
-- Issue #N: Plaid Liabilities

-- Remove column from linked_items first
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'liabilities_last_synced_at'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN liabilities_last_synced_at;
    END IF;
END $$;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS student_loan_liabilities;
DROP TABLE IF EXISTS mortgage_liabilities;
DROP TABLE IF EXISTS credit_liability_aprs;
DROP TABLE IF EXISTS credit_liabilities;
