-- This migration is intentionally irreversible.
--
-- After migration 000087_fix_idempotency_keys_pk.up runs, users may legitimately
-- share the same idempotency key string under different user_ids (the composite
-- PK (user_id, key) allows this). Restoring the old global PRIMARY KEY (key)
-- would require removing duplicate rows, which would silently delete valid
-- idempotency records and break deduplication guarantees.
--
-- To roll back this schema change, restore from a backup taken before this
-- migration was applied, or accept the data loss implications explicitly.
DO $$
BEGIN
    RAISE EXCEPTION
        'Migration 000087 down is intentionally irreversible. '
        'Restoring a global PRIMARY KEY (key) on idempotency_keys is unsafe '
        'after legitimate cross-user duplicate keys may exist. '
        'Restore from backup if rollback is truly required.';
END
$$;
