-- Rollback: Remove trial tracking columns from users table
-- Issue #679: Implement trial service and auto-grant logic

-- Drop the partial index first
DROP INDEX IF EXISTS idx_users_trial_ends_at;

-- Remove trial columns (idempotent)
ALTER TABLE users DROP COLUMN IF EXISTS trial_started_at;
ALTER TABLE users DROP COLUMN IF EXISTS trial_ends_at;
ALTER TABLE users DROP COLUMN IF EXISTS had_trial;
