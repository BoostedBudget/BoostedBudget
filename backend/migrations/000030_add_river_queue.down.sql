-- Migration: Remove River Job Queue Tables
-- Purpose: Rollback River queue setup
--
-- WARNING: This will delete all job data! Use with caution.

-- Drop BoostedBudget-specific table first
DROP TABLE IF EXISTS dead_letter_jobs CASCADE;

-- Drop River tables in reverse dependency order
-- river_client_queue depends on river_client
DROP TABLE IF EXISTS river_client_queue CASCADE;
DROP TABLE IF EXISTS river_client CASCADE;
DROP TABLE IF EXISTS river_queue CASCADE;
DROP TABLE IF EXISTS river_leader CASCADE;

-- river_job depends on river_job_state enum
DROP TABLE IF EXISTS river_job CASCADE;

-- Drop migration tracking table
DROP TABLE IF EXISTS river_migration CASCADE;

-- Drop helper function that uses the enum (must be dropped before enum)
DROP FUNCTION IF EXISTS river_job_state_in_bitmask(BIT(8), river_job_state);

-- Finally drop the enum type (safe now that no dependencies remain)
DROP TYPE IF EXISTS river_job_state;
