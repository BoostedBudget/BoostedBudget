-- Reverse migration 000087_fix_idempotency_keys_pk.
--
-- PRODUCTION WARNING: If any users have accumulated duplicate key strings under
-- different user_ids (valid under the composite PK), those rows will be deleted
-- before the global PK is restored. In test environments the DELETE is a no-op
-- (fresh DB, no existing data). For production rollbacks, prefer restoring from
-- a backup taken before 000087 was applied.

-- Remove the lookup index added by 000087 up
DROP INDEX IF EXISTS idx_idempotency_keys_lookup;

-- Remove the composite PK (user_id, key) added by 000087 up
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey_v2'
          AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey_v2;
    END IF;
END $$;

-- Remove cross-user duplicate key rows that would violate the original global PK.
-- No-op in test environments (table is empty). In production this deletes records
-- — restore from backup if those records must be preserved.
DELETE FROM idempotency_keys a
USING idempotency_keys b
WHERE a.ctid > b.ctid
  AND a.key = b.key;

-- Restore the original global PRIMARY KEY (key) from migration 000086
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idempotency_keys_pkey'
          AND conrelid = 'idempotency_keys'::regclass
    ) THEN
        ALTER TABLE idempotency_keys
            ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);
    END IF;
END $$;
