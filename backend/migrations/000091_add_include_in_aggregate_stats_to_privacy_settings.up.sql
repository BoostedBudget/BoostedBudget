-- #972: Add include_in_aggregate_stats column to privacy_settings table.
--
-- This column controls whether a user's anonymized data is included in
-- aggregate statistics (e.g. spending benchmarks, category averages).
-- Defaults to TRUE (opt-in) but users can opt out via privacy settings.
--
-- Idempotent: wrapped in DO $$ block to check column existence first.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'privacy_settings'
          AND column_name = 'include_in_aggregate_stats'
    ) THEN
        ALTER TABLE privacy_settings
            ADD COLUMN include_in_aggregate_stats BOOLEAN NOT NULL DEFAULT TRUE;
    END IF;
END $$;
