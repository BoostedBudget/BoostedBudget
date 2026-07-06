ALTER TABLE users ADD COLUMN IF NOT EXISTS past_due_started_at TIMESTAMPTZ;

-- Backfill existing past_due rows so the dunning job can act on them.
-- Cap the effective age at 20 days (1 day below the 21-day grace deadline) so that
-- no user is immediately auto-cancelled on the first cron run after this migration.
-- Ops have at least 24 hours to intervene before any user crosses the grace threshold.
UPDATE users
SET past_due_started_at = GREATEST(
    COALESCE(updated_at, NOW()),
    NOW() - INTERVAL '20 days'
)
WHERE subscription_status = 'past_due'
  AND past_due_started_at IS NULL;
