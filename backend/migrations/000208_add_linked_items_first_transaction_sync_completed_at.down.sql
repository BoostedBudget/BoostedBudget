DROP TRIGGER IF EXISTS trg_latch_linked_item_first_transaction_sync_completion ON linked_items;
DROP FUNCTION IF EXISTS latch_linked_item_first_transaction_sync_completion();

ALTER TABLE linked_items
    DROP COLUMN IF EXISTS first_transaction_sync_completed_at;
