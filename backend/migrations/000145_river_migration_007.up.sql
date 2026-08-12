-- Migration: River main-line migration 007
-- Purpose: Bring the hand-transcribed River schema up to what riverdriver/riverpgxv5 v0.43.0
--          expects. Mirrors River's own
--          migration/main/007_notification_outbox_sqlite_jsonb_and_sql_cleanup.up.sql.
--
-- Why this is hand-written: this repo does NOT use River's `rivermigrate` package (grep for it
-- returns nothing). River's schema is transcribed by hand in 000030_add_river_queue.up.sql, which
-- stopped at main-line version 6 (header says "River version: 0.29.x"). Bumping the River library
-- does not update the database, so each new River migration has to be mirrored here.
--
-- Upstream 007 bundles four unrelated changes. Only three affect PostgreSQL:
--   1. Notification outbox table (used by River's notifier).
--   2. SQLite JSONB conversion — NO-OP on PostgreSQL, which already stores these columns as jsonb.
--   3. SQL cleanup: drop river_client / river_client_queue, which River no longer reads or writes
--      (their dbsqlc query files are deleted in riverpgxv5 v0.43.0).
--   4. Add defaults to river_job.max_attempts and river_queue.updated_at.
--
-- IDEMPOTENT: Yes (IF NOT EXISTS / IF EXISTS; ALTER ... SET DEFAULT is naturally idempotent)
-- ROLLBACK: Yes (see 000145_river_migration_007.down.sql)

-- ============================================================================
-- Notification outbox
-- ============================================================================
CREATE TABLE IF NOT EXISTS river_notification (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    payload text NOT NULL,
    topic text NOT NULL,
    CONSTRAINT topic_length CHECK (length(topic) > 0 AND length(topic) < 128)
);

CREATE INDEX IF NOT EXISTS river_notification_created_at_idx ON river_notification (created_at);
CREATE INDEX IF NOT EXISTS river_notification_topic_id_idx ON river_notification (topic, id);

-- ============================================================================
-- SQL cleanup: drop the unused client-tracking tables
-- ============================================================================
-- river_client_queue first — it holds an ON DELETE CASCADE FK to river_client.
-- Squawk's ban-drop-table is bypassed for this file via .squawk.toml excluded_paths; these drops
-- are the whole point of upstream migration 007, and the .down.sql recreates both tables.
DROP TABLE IF EXISTS river_client_queue;
DROP TABLE IF EXISTS river_client;

-- ============================================================================
-- Column defaults
-- ============================================================================
ALTER TABLE river_job
    ALTER COLUMN max_attempts SET DEFAULT 25;

ALTER TABLE river_queue
    ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

-- ============================================================================
-- River bookkeeping
-- ============================================================================
-- Keep river_migration in step with the driver, so River's own version check sees main line 7.
INSERT INTO river_migration (line, version) VALUES ('main', 7)
ON CONFLICT (line, version) DO NOTHING;
