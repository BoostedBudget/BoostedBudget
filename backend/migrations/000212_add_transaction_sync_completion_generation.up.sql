ALTER TABLE linked_items
    ADD COLUMN IF NOT EXISTS transaction_sync_completion_generation BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_linked_items_transaction_sync_completion_generation'
          AND conrelid = 'linked_items'::regclass
    ) THEN
        ALTER TABLE linked_items
            ADD CONSTRAINT chk_linked_items_transaction_sync_completion_generation
            CHECK (transaction_sync_completion_generation >= 0)
            NOT VALID;
    END IF;
END $$;

ALTER TABLE linked_items
    VALIDATE CONSTRAINT chk_linked_items_transaction_sync_completion_generation;
