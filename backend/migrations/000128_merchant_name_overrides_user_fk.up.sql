-- Issue #1440: merchant_name_overrides.user_id has never had a foreign key to users(id) — the
-- only user-scoped table in the schema missing one (verified by enumerating every FK that
-- references users(id): 36 ON DELETE CASCADE + 2 ON DELETE SET NULL, none RESTRICT/NO ACTION,
-- and merchant_name_overrides.user_id was not among them).
--
-- History: migration 000101 (Issue #957) created the column as TEXT with no FK at all.
-- Migration 000103 (Issue #1348) fixed the resulting TEXT/UUID operator-mismatch bug by
-- converting the column to UUID, but no migration ever added the missing constraint itself —
-- an oversight, not a deliberate design choice; every sibling per-user table in this schema has
-- an ON DELETE CASCADE FK to users(id).
--
-- Why this matters now: Issue #1440 replaces DeleteUserCascade's soft-delete-plus-five-explicit-
-- DELETEs with a real `DELETE FROM users` and lets the schema's FK graph do the erasure. Without
-- this FK, hard-deleting a user would leave their per-merchant display-name/merge overrides
-- behind forever — the one gap in an otherwise-complete FK graph.
--
-- Safety: verified zero orphaned rows exist today. Every merchant_name_overrides.user_id value
-- is written from an authenticated user's own id, and account deletion has never actually hard-
-- deleted the referenced users row (the pre-#1440 cascade only ever soft-deleted it), so no
-- dangling value can have accumulated. The guard below re-verifies this defensively before adding
-- the constraint. NOT VALID + VALIDATE keeps the initial lock brief regardless.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM merchant_name_overrides m
        WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = m.user_id)
    ) THEN
        RAISE EXCEPTION 'Migration 000128: merchant_name_overrides has orphaned user_id values — cannot add FK safely.';
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_merchant_name_overrides_user_id'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            ADD CONSTRAINT fk_merchant_name_overrides_user_id
            FOREIGN KEY (user_id) REFERENCES users(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;

ALTER TABLE merchant_name_overrides VALIDATE CONSTRAINT fk_merchant_name_overrides_user_id;
