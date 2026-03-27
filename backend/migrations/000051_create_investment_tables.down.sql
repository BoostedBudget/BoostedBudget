-- Rollback: Create investment tables
-- Issue #659: Plaid Investments

-- Remove column from linked_items first
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'investments_last_synced_date'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN investments_last_synced_date;
    END IF;
END $$;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS investment_transactions;
DROP TABLE IF EXISTS holdings;
DROP TABLE IF EXISTS securities;
