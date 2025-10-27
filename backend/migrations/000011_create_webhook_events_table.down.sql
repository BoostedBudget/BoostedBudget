-- Drop webhook_events table (rollback for migration 000011)
-- Issue #202: Plaid Webhook Integration

DROP TABLE IF EXISTS webhook_events CASCADE;
