ALTER TABLE linked_items
    ADD COLUMN IF NOT EXISTS first_transaction_sync_completed_at TIMESTAMPTZ;

ALTER TABLE linked_items
    ADD COLUMN IF NOT EXISTS transaction_sync_state TEXT;

ALTER TABLE linked_items
    ALTER COLUMN transaction_sync_state SET DEFAULT 'legacy_ambiguous';
