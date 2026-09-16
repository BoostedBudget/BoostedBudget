DO $$
DECLARE completions_exist BOOLEAN;
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'linked_items'
          AND column_name = 'transaction_sync_completion_generation'
    ) THEN
        EXECUTE 'SELECT EXISTS (
            SELECT 1 FROM linked_items
            WHERE transaction_sync_completion_generation <> 0
        )' INTO completions_exist;
        IF completions_exist THEN
            RAISE EXCEPTION 'REFUSING TO ROLL BACK 000212: transaction sync completion generations exist';
        END IF;
    END IF;
END $$;

ALTER TABLE linked_items
    DROP CONSTRAINT IF EXISTS chk_linked_items_transaction_sync_completion_generation,
    DROP COLUMN IF EXISTS transaction_sync_completion_generation;
