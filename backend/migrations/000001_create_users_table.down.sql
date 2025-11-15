-- Rollback: Drop users table and all dependent objects
-- WARNING: This will delete ALL user data
-- Only use in development or with full backup

DROP TABLE IF EXISTS users CASCADE;
