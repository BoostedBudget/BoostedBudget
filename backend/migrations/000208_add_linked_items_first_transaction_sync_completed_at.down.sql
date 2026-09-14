ALTER TABLE linked_items
    DROP COLUMN IF EXISTS transaction_sync_state,
    DROP COLUMN IF EXISTS first_transaction_sync_completed_at;
