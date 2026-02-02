-- Create paddle_webhook_events table for audit trail and idempotency
-- Mirrors webhook_events pattern but adapted for Paddle's webhook structure

CREATE TABLE IF NOT EXISTS paddle_webhook_events (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Paddle webhook identification
    event_id VARCHAR(255) NOT NULL UNIQUE, -- Paddle's unique event ID (e.g., evt_01h...)
    event_type VARCHAR(100) NOT NULL, -- Event type: subscription.created, transaction.completed, etc.

    -- Complete webhook payload (JSONB for flexibility)
    payload JSONB NOT NULL, -- Full webhook body, forward compatible with new fields

    -- Security and verification (HMAC-SHA256, not JWT like Plaid)
    signature TEXT, -- Value from Paddle-Signature header
    signature_valid BOOLEAN NOT NULL DEFAULT false, -- Result of HMAC-SHA256 verification

    -- Processing state
    processed BOOLEAN NOT NULL DEFAULT false, -- Has this webhook been processed?
    processed_at TIMESTAMP WITH TIME ZONE, -- When was it processed?
    processing_error TEXT, -- Error message if processing failed
    processing_attempts INTEGER NOT NULL DEFAULT 0, -- Number of processing attempts (for retry logic)

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient querying
CREATE INDEX idx_paddle_webhook_events_event_type ON paddle_webhook_events(event_type);
CREATE INDEX idx_paddle_webhook_events_created_at ON paddle_webhook_events(created_at DESC); -- Most recent first
CREATE INDEX idx_paddle_webhook_events_processed ON paddle_webhook_events(processed) WHERE processed = false; -- Unprocessed webhooks
CREATE INDEX idx_paddle_webhook_events_event_id ON paddle_webhook_events(event_id); -- Fast duplicate detection

-- Trigger to automatically update updated_at timestamp
CREATE TRIGGER update_paddle_webhook_events_updated_at
    BEFORE UPDATE ON paddle_webhook_events
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE paddle_webhook_events IS 'Audit log of all Paddle webhook notifications with HMAC signature verification and processing status';
COMMENT ON COLUMN paddle_webhook_events.event_id IS 'Unique event ID from Paddle (e.g., evt_01h...) - used for idempotency';
COMMENT ON COLUMN paddle_webhook_events.event_type IS 'Type of event: subscription.created, transaction.completed, etc.';
COMMENT ON COLUMN paddle_webhook_events.payload IS 'Complete webhook payload as JSON for forward compatibility';
COMMENT ON COLUMN paddle_webhook_events.signature IS 'HMAC-SHA256 signature from Paddle-Signature header';
COMMENT ON COLUMN paddle_webhook_events.signature_valid IS 'Result of HMAC-SHA256 signature verification - monitor for false values (security concern)';
COMMENT ON COLUMN paddle_webhook_events.processed IS 'Has this webhook been processed? Used for idempotency and manual replay';
COMMENT ON COLUMN paddle_webhook_events.processing_error IS 'Error message if webhook processing failed (for debugging)';
COMMENT ON COLUMN paddle_webhook_events.processing_attempts IS 'Number of times we attempted to process this webhook (for retry tracking)';
