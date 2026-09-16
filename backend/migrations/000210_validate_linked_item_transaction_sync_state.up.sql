ALTER TABLE linked_items
    VALIDATE CONSTRAINT chk_linked_items_transaction_sync_state,
    VALIDATE CONSTRAINT chk_linked_items_completed_sync_timestamp;
