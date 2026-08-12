-- Issue #1514: Bounded audit signal for rejected (unsigned or invalid-signature) webhook
-- deliveries. Unsigned/invalid Paddle and Plaid webhooks are no longer persisted into
-- paddle_webhook_events / webhook_events (that table's UNIQUE event_id / full payload columns
-- are exactly the idempotency-poisoning vector this table avoids). Instead we record only
-- bounded, non-identifying metadata here: provider, body length, and a SHA-256 digest of the
-- body — never the raw payload and never an attacker-chosen identifier in a unique column.
--
-- Row count is capped at application level (see repository.invalidWebhookAttemptCap): every
-- insert is followed, in the same transaction, by a trim back to the cap so this table can
-- never grow unboundedly even under sustained attack.
CREATE TABLE IF NOT EXISTS invalid_webhook_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Source of the rejected delivery: 'paddle' or 'plaid'.
    provider VARCHAR(50) NOT NULL,

    -- Length of the rejected request body in bytes. Bounded metadata only — never the body itself.
    body_length INTEGER NOT NULL,

    -- Hex-encoded SHA-256 digest of the rejected request body. Lets an operator confirm whether
    -- two rejected attempts carried the same body without ever storing that body.
    body_sha256 VARCHAR(64) NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_invalid_webhook_attempts_created_at
    ON invalid_webhook_attempts (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_invalid_webhook_attempts_provider
    ON invalid_webhook_attempts (provider);

COMMENT ON TABLE invalid_webhook_attempts IS 'Bounded (row-capped) audit trail of rejected unsigned/invalid-signature webhook deliveries. Metadata only — no raw payload, no attacker-chosen identifiers. Issue #1514.';
COMMENT ON COLUMN invalid_webhook_attempts.provider IS 'Webhook source: paddle or plaid';
COMMENT ON COLUMN invalid_webhook_attempts.body_length IS 'Length of the rejected request body in bytes';
COMMENT ON COLUMN invalid_webhook_attempts.body_sha256 IS 'Hex-encoded SHA-256 digest of the rejected request body';
