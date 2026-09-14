ALTER TABLE linked_items
    DROP CONSTRAINT IF EXISTS chk_linked_items_completed_sync_timestamp,
    DROP CONSTRAINT IF EXISTS chk_linked_items_transaction_sync_state;
