ALTER TABLE linked_items
    ADD COLUMN IF NOT EXISTS first_transaction_sync_completed_at TIMESTAMPTZ;

-- Dual-write during rolling deploys: an older server finalizes a successful sync by updating
-- last_synced_at. Latch that evidence too, so a newer server cannot later emit a duplicate initial
-- completion. The trigger can remain as a compatibility invariant after rollout.
CREATE OR REPLACE FUNCTION latch_linked_item_first_transaction_sync_completion()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.last_synced_at IS NOT NULL
       AND NEW.last_synced_at IS DISTINCT FROM OLD.last_synced_at THEN
        NEW.first_transaction_sync_completed_at = COALESCE(
            NEW.first_transaction_sync_completed_at,
            NEW.last_synced_at
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_latch_linked_item_first_transaction_sync_completion ON linked_items;
CREATE TRIGGER trg_latch_linked_item_first_transaction_sync_completion
    BEFORE UPDATE OF last_synced_at ON linked_items
    FOR EACH ROW
    EXECUTE FUNCTION latch_linked_item_first_transaction_sync_completion();

-- Existing last_synced_at values are durable evidence that this connection already completed at
-- least one transaction sync. New and relinked connections remain NULL until their own first full
-- catch-up.
UPDATE linked_items
SET first_transaction_sync_completed_at = last_synced_at
WHERE first_transaction_sync_completed_at IS NULL
  AND last_synced_at IS NOT NULL;
