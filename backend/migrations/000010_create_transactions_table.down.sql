-- Rollback: Drop transactions table
-- WARNING: This will delete ALL transaction data
-- Only use in development or with full backup

DROP TABLE IF EXISTS transactions CASCADE;
