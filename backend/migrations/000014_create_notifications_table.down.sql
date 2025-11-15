-- Rollback: Drop notifications table
-- WARNING: This will delete ALL user notifications
-- Only use in development or with full backup

DROP TABLE IF EXISTS notifications CASCADE;
