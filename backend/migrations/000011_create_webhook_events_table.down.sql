-- Rollback: Drop webhook_events table
-- WARNING: This will delete ALL webhook event history
-- Only use in development or with full backup

DROP TABLE IF EXISTS webhook_events CASCADE;
