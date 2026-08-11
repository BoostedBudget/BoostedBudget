-- Rollback: River main-line migration 007
-- Mirrors riverdriver/riverpgxv5 v0.43.0
-- migration/main/007_notification_outbox_sqlite_jsonb_and_sql_cleanup.down.sql.
--
-- IDEMPOTENT: Yes (IF NOT EXISTS / IF EXISTS)

-- ============================================================================
-- River bookkeeping
-- ============================================================================
DELETE FROM river_migration WHERE line = 'main' AND version = 7;

-- ============================================================================
-- Revert column defaults
-- ============================================================================
ALTER TABLE river_queue
    ALTER COLUMN updated_at DROP DEFAULT;

ALTER TABLE river_job
    ALTER COLUMN max_attempts DROP DEFAULT;

-- ============================================================================
-- Restore the client-tracking tables
-- ============================================================================
-- Definitions copied verbatim from 000030_add_river_queue.up.sql (River main migration 005), so a
-- down-then-up cycle lands back on the same schema the 000030..000144 range produced.
CREATE UNLOGGED TABLE IF NOT EXISTS river_client (
    id text PRIMARY KEY NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}',
    paused_at timestamptz,
    updated_at timestamptz NOT NULL,
    CONSTRAINT name_length CHECK (char_length(id) > 0 AND char_length(id) < 128)
);

CREATE UNLOGGED TABLE IF NOT EXISTS river_client_queue (
    river_client_id text NOT NULL REFERENCES river_client (id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    max_workers bigint NOT NULL DEFAULT 0,
    metadata jsonb NOT NULL DEFAULT '{}',
    num_jobs_completed bigint NOT NULL DEFAULT 0,
    num_jobs_running bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (river_client_id, name),
    CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 128),
    CONSTRAINT num_jobs_completed_zero_or_positive CHECK (num_jobs_completed >= 0),
    CONSTRAINT num_jobs_running_zero_or_positive CHECK (num_jobs_running >= 0)
);

-- ============================================================================
-- Drop the notification outbox
-- ============================================================================
DROP TABLE IF EXISTS river_notification;
