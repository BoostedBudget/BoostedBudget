-- Rollback: Drop accounts table
-- WARNING: This will delete ALL linked bank accounts
-- Only use in development or with full backup

DROP TABLE IF EXISTS accounts CASCADE;
