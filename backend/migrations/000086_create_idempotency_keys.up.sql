-- Server-side idempotency keys for MCP write operations (Issue #1087 GAP-3).
-- The Worker injects an Idempotency-Key header; the backend checks this table
-- before executing the write and returns the cached response if the key exists.
-- Keys expire after 24 hours and are cleaned up by a periodic maintenance job.

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key          TEXT        NOT NULL,
    user_id      UUID        NOT NULL,
    operation_id TEXT        NOT NULL,
    response_body JSONB      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',

    CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_user_expires
    ON idempotency_keys (user_id, expires_at);
