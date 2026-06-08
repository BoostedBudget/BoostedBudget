-- #972: Add timezone and timezone_source columns to users table.
--
-- timezone: IANA timezone string (e.g. America/Los_Angeles) used by the daily
-- budget-period-close scanner to compute period boundaries in the user's local
-- timezone rather than UTC. Prevents up-to-1-day boundary skew.
--
-- timezone_source: Tracks how the timezone was set; values:
--   - 'default': never set by user
--   - 'auto': browser-detected
--   - 'user': explicitly chosen
-- This prevents auto-capture from overwriting an explicit user choice.
-- Source transitions: default → auto → user (once 'user', auto-capture never overwrites).
--
-- Idempotent: wrapped in DO $$ block to check column existence first.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'users'
          AND column_name = 'timezone'
    ) THEN
        ALTER TABLE users
            ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'users'
          AND column_name = 'timezone_source'
    ) THEN
        ALTER TABLE users
            ADD COLUMN timezone_source TEXT NOT NULL DEFAULT 'default';
    END IF;
END $$;
