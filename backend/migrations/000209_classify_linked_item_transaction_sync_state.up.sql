-- Classify only the NULL snapshot left by migration 000208. Inserts from an older binary after
-- 000208 already receive legacy_ambiguous from the default and must not be reclassified from later
-- last_synced_at activity. Keeping this scan out of 000208 releases its ACCESS EXCLUSIVE DDL lock
-- before the backfill runs.
UPDATE linked_items
SET first_transaction_sync_completed_at = COALESCE(
        first_transaction_sync_completed_at,
        last_synced_at
    ),
    transaction_sync_state = CASE
        WHEN first_transaction_sync_completed_at IS NOT NULL OR last_synced_at IS NOT NULL
            THEN 'completed'
        ELSE 'legacy_ambiguous'
    END
WHERE transaction_sync_state IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_linked_items_transaction_sync_state'
          AND conrelid = 'linked_items'::regclass
    ) THEN
        ALTER TABLE linked_items
            ADD CONSTRAINT chk_linked_items_transaction_sync_state
            CHECK (
                transaction_sync_state IS NOT NULL
                AND transaction_sync_state IN ('legacy_ambiguous', 'never_synced', 'completed')
            )
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_linked_items_completed_sync_timestamp'
          AND conrelid = 'linked_items'::regclass
    ) THEN
        ALTER TABLE linked_items
            ADD CONSTRAINT chk_linked_items_completed_sync_timestamp
            CHECK (
                transaction_sync_state <> 'completed'
                OR first_transaction_sync_completed_at IS NOT NULL
            )
            NOT VALID;
    END IF;
END $$;
