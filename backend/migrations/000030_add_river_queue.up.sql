-- Migration: Add River Job Queue Tables
-- Purpose: Sets up River's PostgreSQL-backed job queue for async processing
-- River version: 0.29.x
--
-- IDEMPOTENT: Yes (uses IF NOT EXISTS, DO $$ blocks)
-- ROLLBACK: Yes (see 000030_add_river_queue.down.sql)

-- ============================================================================
-- River Migration 001: Create migration tracking table
-- ============================================================================
CREATE TABLE IF NOT EXISTS river_migration (
    line TEXT NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT line_length CHECK (char_length(line) > 0 AND char_length(line) < 128),
    CONSTRAINT version_gte_1 CHECK (version >= 1),
    PRIMARY KEY (line, version)
);

-- ============================================================================
-- River Migration 002: Create job state enum and main job table
-- ============================================================================
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'river_job_state') THEN
        CREATE TYPE river_job_state AS ENUM(
            'available',
            'cancelled',
            'completed',
            'discarded',
            'pending',
            'retryable',
            'running',
            'scheduled'
        );
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS river_job (
    -- 8 bytes
    id bigserial PRIMARY KEY,

    -- 8 bytes (4 bytes + 2 bytes + 2 bytes)
    state river_job_state NOT NULL DEFAULT 'available',
    attempt smallint NOT NULL DEFAULT 0,
    max_attempts smallint NOT NULL,

    -- 8 bytes each (no alignment needed)
    attempted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    finalized_at timestamptz,
    scheduled_at timestamptz NOT NULL DEFAULT NOW(),

    -- 2 bytes
    priority smallint NOT NULL DEFAULT 1,

    -- types stored out-of-band
    args jsonb NOT NULL,
    attempted_by text[],
    errors jsonb[],
    kind text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    queue text NOT NULL DEFAULT 'default',
    tags varchar(255)[] NOT NULL DEFAULT '{}',

    -- Unique job constraints (added in River 005/006)
    unique_key bytea,
    unique_states BIT(8),

    CONSTRAINT finalized_or_finalized_at_null CHECK (
        (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
        (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))
    ),
    CONSTRAINT max_attempts_is_positive CHECK (max_attempts > 0),
    CONSTRAINT priority_in_range CHECK (priority >= 1 AND priority <= 4),
    CONSTRAINT queue_length CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
    CONSTRAINT kind_length CHECK (char_length(kind) > 0 AND char_length(kind) < 128)
);

-- Indexes for efficient job processing
CREATE INDEX IF NOT EXISTS river_job_kind ON river_job USING btree(kind);
CREATE INDEX IF NOT EXISTS river_job_state_and_finalized_at_index ON river_job USING btree(state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS river_job_prioritized_fetching_index ON river_job USING btree(state, queue, priority, scheduled_at, id);
CREATE INDEX IF NOT EXISTS river_job_args_index ON river_job USING GIN(args);
CREATE INDEX IF NOT EXISTS river_job_metadata_index ON river_job USING GIN(metadata);

-- ============================================================================
-- River Migration 004: Create queue management table
-- ============================================================================
CREATE TABLE IF NOT EXISTS river_queue (
    name text PRIMARY KEY NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}' ::jsonb,
    paused_at timestamptz,
    updated_at timestamptz NOT NULL
);

-- ============================================================================
-- River Migration 002: Create leader election table (unlogged for performance)
-- ============================================================================
CREATE UNLOGGED TABLE IF NOT EXISTS river_leader (
    elected_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    leader_id text NOT NULL,
    name text PRIMARY KEY DEFAULT 'default',
    CONSTRAINT name_length CHECK (name = 'default'),
    CONSTRAINT leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

-- ============================================================================
-- River Migration 005: Create client tracking tables (unlogged for performance)
-- ============================================================================
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
-- River Migration 006: Unique job state bitmask function
-- ============================================================================
CREATE OR REPLACE FUNCTION river_job_state_in_bitmask(bitmask BIT(8), state river_job_state)
RETURNS boolean
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending'   THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running'   THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0
    END = 1;
$$;

-- Unique job index using bitmask function
CREATE UNIQUE INDEX IF NOT EXISTS river_job_unique_idx ON river_job (unique_key)
    WHERE unique_key IS NOT NULL
      AND unique_states IS NOT NULL
      AND river_job_state_in_bitmask(unique_states, state);

-- ============================================================================
-- Dead Letter Queue Table (BoostedBudget-specific)
-- Stores jobs that have exhausted all retry attempts for manual review
-- ============================================================================
CREATE TABLE IF NOT EXISTS dead_letter_jobs (
    id BIGSERIAL PRIMARY KEY,
    original_job_id BIGINT NOT NULL,
    kind VARCHAR(255) NOT NULL,
    queue VARCHAR(255) NOT NULL,
    args JSONB NOT NULL,
    errors JSONB NOT NULL,
    max_attempts INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- BoostedBudget-specific fields
    user_id VARCHAR(255),           -- User who owns the failed job (for notification)
    notified BOOLEAN DEFAULT FALSE, -- Whether user has been notified
    retried_at TIMESTAMPTZ,         -- When job was manually retried
    retry_job_id BIGINT,            -- ID of new job if retried
    notes TEXT                      -- Admin notes about the failure
);

-- Indexes for dead letter queue queries
CREATE INDEX IF NOT EXISTS idx_dead_letter_user_id ON dead_letter_jobs(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dead_letter_kind ON dead_letter_jobs(kind);
CREATE INDEX IF NOT EXISTS idx_dead_letter_failed_at ON dead_letter_jobs(failed_at);
CREATE INDEX IF NOT EXISTS idx_dead_letter_notified ON dead_letter_jobs(notified) WHERE notified = FALSE;

-- ============================================================================
-- Record River migration versions (for River's internal tracking)
-- ============================================================================
INSERT INTO river_migration (line, version) VALUES
    ('main', 1),
    ('main', 2),
    ('main', 3),
    ('main', 4),
    ('main', 5),
    ('main', 6)
ON CONFLICT (line, version) DO NOTHING;
