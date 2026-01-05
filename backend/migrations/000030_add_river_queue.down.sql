-- Migration: Remove River Job Queue Tables
-- Purpose: Rollback River queue setup
--
-- WARNING: This will delete all job data! Use with caution.

-- ============================================================================
-- Drop BoostedBudget-specific tables
-- ============================================================================
DROP TABLE IF EXISTS dead_letter_jobs;

-- ============================================================================
-- Drop River client tracking tables
-- ============================================================================
DROP TABLE IF EXISTS river_client_queue;
DROP TABLE IF EXISTS river_client;

-- ============================================================================
-- Drop River main tables
-- ============================================================================
DROP TABLE IF EXISTS river_queue;
DROP TABLE IF EXISTS river_leader;

-- Drop unique job index and function
DROP INDEX IF EXISTS river_job_unique_idx;
DROP FUNCTION IF EXISTS river_job_state_in_bitmask;

-- Drop job table indexes
DROP INDEX IF EXISTS river_job_metadata_index;
DROP INDEX IF EXISTS river_job_args_index;
DROP INDEX IF EXISTS river_job_prioritized_fetching_index;
DROP INDEX IF EXISTS river_job_state_and_finalized_at_index;
DROP INDEX IF EXISTS river_job_kind;

-- Drop main job table
DROP TABLE IF EXISTS river_job;

-- Drop job state enum
DROP TYPE IF EXISTS river_job_state;

-- Drop migration tracking table
DROP TABLE IF EXISTS river_migration;
