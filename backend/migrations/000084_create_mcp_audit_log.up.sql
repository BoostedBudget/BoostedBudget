-- MCP security audit log (Issue #1087)
-- Every MCP operation is logged here. Write operations have their audit row
-- written in the same transaction as the write itself (fail-closed).
-- No raw financial payload columns — only operation class/id and outcome.

CREATE TABLE IF NOT EXISTS mcp_audit_log (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ts                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor_user_id           UUID NOT NULL,
    grant_id                TEXT NOT NULL,
    scopes                  TEXT[] NOT NULL DEFAULT '{}',
    operation_class         TEXT NOT NULL CHECK (operation_class IN ('read', 'write')),
    operation_id            TEXT NOT NULL,
    confirmation_presented  BOOLEAN NOT NULL DEFAULT FALSE,
    confirmation_confirmed  BOOLEAN NOT NULL DEFAULT FALSE,
    result                  TEXT NOT NULL CHECK (result IN ('success', 'denied', 'error', 'confirmation_required')),
    idempotency_key         TEXT,
    revocation_state        TEXT,
    request_id              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_actor_ts
    ON mcp_audit_log (actor_user_id, ts DESC);

CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_operation_id
    ON mcp_audit_log (operation_id, ts DESC);
