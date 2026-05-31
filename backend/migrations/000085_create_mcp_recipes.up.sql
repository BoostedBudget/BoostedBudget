-- MCP saved recipes (Issue #1087)
-- Declarative JSONB specs for repeatable agent workflows.
-- v1: read-only compositions only (spec validation enforces this at the service layer).
-- Write-recipes deferred to a follow-up issue (need standing-auth model).

CREATE TABLE IF NOT EXISTS mcp_recipes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    -- Constrained DSL: list of allowlisted read operations + bound params.
    -- Never raw TypeScript. Service layer validates no write-class operations
    -- appear in the spec (v1 read-only constraint).
    spec        JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Optional cron schedule (e.g. "0 9 * * 1" = every Monday 9am).
    -- Null = manual-run only.
    schedule    TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT mcp_recipes_user_id_name_unique UNIQUE (user_id, name)
);

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'mcp_recipes_user_id_fkey'
    ) THEN
        ALTER TABLE mcp_recipes
            ADD CONSTRAINT mcp_recipes_user_id_fkey
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_mcp_recipes_user_id
    ON mcp_recipes (user_id);

CREATE INDEX IF NOT EXISTS idx_mcp_recipes_user_enabled
    ON mcp_recipes (user_id, enabled)
    WHERE enabled = TRUE;
