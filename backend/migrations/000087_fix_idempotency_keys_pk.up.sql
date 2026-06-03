-- R4-6: Fix idempotency_keys PRIMARY KEY to be (user_id, key) composite.
--
-- The original migration (000086) used a global `key` PK, which caused a cross-tenant
-- collision: if two users happened to use the same key string, one would silently fail
-- to store (ON CONFLICT DO NOTHING) and their future GET would miss. Since the lookup
-- was scoped to (key, user_id), user B couldn't see user A's response, but idempotency
-- for user B was broken silently.
--
-- Fix: replace the global PK with (user_id, key) and update ON CONFLICT accordingly.
-- We also add a partial index on (key, user_id, expires_at) for the GET lookup path.

DO $$
BEGIN
    -- Drop old global PK only if it exists (idempotent)
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey'
        AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        -- Remove the global PK constraint
        ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
    END IF;

    -- Add composite PK (user_id, key) — per-user key uniqueness
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey_v2'
        AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        ALTER TABLE idempotency_keys
            ADD CONSTRAINT idempotency_keys_pkey_v2 PRIMARY KEY (user_id, key);
    END IF;
END
$$;

-- Efficient lookup by (key, user_id, expires_at) — used in the GET query
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_lookup
    ON idempotency_keys (key, user_id, expires_at);
