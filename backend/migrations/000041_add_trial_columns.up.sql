-- Migration: Add trial tracking columns to users table
-- Issue #679: Implement trial service and auto-grant logic
--
-- Adds columns for tracking trial status:
-- - trial_started_at: When the trial was granted
-- - trial_ends_at: When the trial expires (typically 60 days from start)
-- - had_trial: Whether user has ever had a trial (prevents re-granting)

-- Add trial columns (idempotent)
ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_started_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_ends_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS had_trial BOOLEAN NOT NULL DEFAULT false;

-- Create index for efficient trial expiration queries
CREATE INDEX IF NOT EXISTS idx_users_trial_ends_at ON users(trial_ends_at)
WHERE subscription_status = 'trial' AND trial_ends_at IS NOT NULL;

-- Backfill had_trial for existing trial users
UPDATE users
SET had_trial = true
WHERE subscription_status = 'trial' AND had_trial = false;

COMMENT ON COLUMN users.trial_started_at IS 'When the trial was granted to the user';
COMMENT ON COLUMN users.trial_ends_at IS 'When the trial expires (60 days from trial_started_at)';
COMMENT ON COLUMN users.had_trial IS 'Whether user has ever had a trial (prevents re-granting)';
