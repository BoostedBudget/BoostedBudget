-- Issue #1514: Rollback for invalid_webhook_attempts audit table.
DROP TABLE IF EXISTS invalid_webhook_attempts;
