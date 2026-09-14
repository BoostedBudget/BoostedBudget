ALTER TABLE linked_items
    ADD COLUMN IF NOT EXISTS first_transaction_sync_completed_at TIMESTAMPTZ;

-- Treat existing last_synced_at values as the rollout baseline so pre-cutover connections are not
-- re-announced as initial. New and relinked connections remain NULL until their own first full
-- catch-up; post-snapshot legacy writes are handled conservatively by the repository disposition.
UPDATE linked_items
SET first_transaction_sync_completed_at = last_synced_at
WHERE first_transaction_sync_completed_at IS NULL
  AND last_synced_at IS NOT NULL;
