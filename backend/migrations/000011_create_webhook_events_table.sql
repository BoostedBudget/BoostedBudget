-- Create webhook_events table for logging Plaid webhook notifications
-- Issue #202: Plaid Webhook Integration
-- Purpose: Audit trail, debugging, GDPR compliance, webhook replay capability
--
-- This table stores all incoming webhook notifications from Plaid, including:
-- - Signature verification results for security monitoring
-- - Complete payload as JSONB for flexibility with new webhook types
-- - Processing status for idempotency and manual replay
-- - Extracted metadata fields for efficient querying

CREATE TABLE IF NOT EXISTS webhook_events (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Webhook identification (from Plaid payload)
    webhook_type VARCHAR(100) NOT NULL, -- 'TRANSACTIONS', 'ITEM', 'AUTH', etc.
    webhook_code VARCHAR(100), -- 'DEFAULT_UPDATE', 'ERROR', 'INITIAL_UPDATE', etc.
    item_id VARCHAR(255), -- Plaid item_id (may be null for some webhook types)

    -- Error tracking (from Plaid error object)
    error_code VARCHAR(100), -- Plaid error code if webhook reports an error

    -- Transaction-specific fields (only populated for TRANSACTIONS webhooks)
    new_transactions INTEGER, -- Count of new transactions available
    removed_transactions INTEGER, -- Count of transactions removed

    -- Complete webhook payload (JSONB for flexibility)
    payload JSONB NOT NULL, -- Full webhook body, forward compatible with new fields

    -- Security and verification
    signature VARCHAR(1000), -- Value from Plaid-Verification header (JWT)
    signature_valid BOOLEAN NOT NULL DEFAULT false, -- Result of JWT verification

    -- Processing state
    processed BOOLEAN NOT NULL DEFAULT false, -- Has this webhook been processed?
    processed_at TIMESTAMP WITH TIME ZONE, -- When was it processed?

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient querying
CREATE INDEX idx_webhook_events_webhook_type ON webhook_events(webhook_type);
CREATE INDEX idx_webhook_events_item_id ON webhook_events(item_id);
CREATE INDEX idx_webhook_events_created_at ON webhook_events(created_at DESC); -- Most recent first
CREATE INDEX idx_webhook_events_processed ON webhook_events(processed) WHERE processed = false; -- Unprocessed webhooks

-- Trigger to automatically update updated_at timestamp
CREATE TRIGGER update_webhook_events_updated_at
    BEFORE UPDATE ON webhook_events
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE webhook_events IS 'Audit log of all Plaid webhook notifications with signature verification and processing status';
COMMENT ON COLUMN webhook_events.webhook_type IS 'Type of webhook: TRANSACTIONS, ITEM, AUTH, etc.';
COMMENT ON COLUMN webhook_events.webhook_code IS 'Specific event code: DEFAULT_UPDATE, ERROR, INITIAL_UPDATE, etc.';
COMMENT ON COLUMN webhook_events.payload IS 'Complete webhook payload as JSON for forward compatibility';
COMMENT ON COLUMN webhook_events.signature IS 'JWT from Plaid-Verification header';
COMMENT ON COLUMN webhook_events.signature_valid IS 'Result of ES256 JWT signature verification - monitor for false values (security concern)';
COMMENT ON COLUMN webhook_events.processed IS 'Has this webhook been processed? Used for idempotency and manual replay';
