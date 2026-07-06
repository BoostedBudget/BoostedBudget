ALTER TABLE users ADD COLUMN IF NOT EXISTS past_due_started_at TIMESTAMPTZ;

-- Backfill existing past_due rows so the dunning job can act on them.
-- Use updated_at as the best available proxy for when they entered past_due.
UPDATE users
SET past_due_started_at = COALESCE(updated_at, NOW())
WHERE subscription_status = 'past_due'
  AND past_due_started_at IS NULL;
