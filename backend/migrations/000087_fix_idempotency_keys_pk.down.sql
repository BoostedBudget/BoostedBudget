-- Rollback: restore global PK on idempotency_keys (reverts 000087 up).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey_v2'
        AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey_v2;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey'
        AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        ALTER TABLE idempotency_keys ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_idempotency_keys_lookup;
