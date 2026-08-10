-- Rollback for 000130_user_subscription_overrides_timestamptz.up.sql.
--
-- Restores user_subscription_overrides.expires_at/created_at/updated_at to the ORIGINAL naked
-- TIMESTAMP (no time zone) definition from 000004_create_subscription_overrides_table.up.sql,
-- including its DEFAULT CURRENT_TIMESTAMP on created_at/updated_at. This is a genuine restoration
-- of the pre-000130 schema, warts and all — the tzHazardAllowlist entry for 000004 in
-- migration_tz_hazard_scan_test.go documents exactly this pattern and is NOT removed by this
-- rollback, because 000004's own already-applied SQL text is unchanged by any of this.
--
-- Honesty check: this restores the column TYPE, not any information 000130's UTC reinterpretation
-- may have discarded. If some historical row's naked wall-clock value was, in fact, written under
-- a non-UTC session (this repo has no record of any writer that would have done so — see the up
-- migration's comment — but this rollback cannot prove a negative for all of history), 000130's
-- `AT TIME ZONE 'UTC'` USING clause already collapsed that value to a single UTC-interpreted
-- instant when it ran; converting the type back to TIMESTAMP here only re-exposes that same
-- (already-collapsed) instant as a naked wall-clock value under whatever zone happens to read it
-- next. Rolling back reverses the SCHEMA, not the semantics of data 000130 already reinterpreted.
-- Idempotent by the same argument as the up migration's guard (see that file's header comment):
-- all three columns are converted together inside one DO block, which `psql -f -` sends and
-- executes as a single autocommitted statement, so only two states are reachable by re-running
-- this file — all three columns still TIMESTAMPTZ (convert), or all three already naked
-- TIMESTAMP (no-op). Any OTHER combination is treated as an error rather than silently no-oping
-- or silently re-converting an already-naked column.
DO $$
DECLARE
    created_at_type TEXT;
    updated_at_type TEXT;
    expires_at_type TEXT;
BEGIN
    SELECT data_type INTO created_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'created_at'
          AND table_schema = current_schema();
    SELECT data_type INTO updated_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'updated_at'
          AND table_schema = current_schema();
    SELECT data_type INTO expires_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'expires_at'
          AND table_schema = current_schema();

    IF created_at_type = 'timestamp without time zone'
       AND updated_at_type = 'timestamp without time zone'
       AND expires_at_type = 'timestamp without time zone' THEN
        -- Already rolled back (e.g. a prior application of this file) — no-op.
        NULL;
    ELSIF created_at_type = 'timestamp with time zone'
       AND updated_at_type = 'timestamp with time zone'
       AND expires_at_type = 'timestamp with time zone' THEN
        ALTER TABLE user_subscription_overrides ALTER COLUMN created_at DROP DEFAULT;
        ALTER TABLE user_subscription_overrides ALTER COLUMN updated_at DROP DEFAULT;

        ALTER TABLE user_subscription_overrides
            ALTER COLUMN expires_at TYPE TIMESTAMP USING (expires_at AT TIME ZONE 'UTC');
        ALTER TABLE user_subscription_overrides
            ALTER COLUMN created_at TYPE TIMESTAMP USING (created_at AT TIME ZONE 'UTC');
        ALTER TABLE user_subscription_overrides
            ALTER COLUMN updated_at TYPE TIMESTAMP USING (updated_at AT TIME ZONE 'UTC');

        ALTER TABLE user_subscription_overrides ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
        ALTER TABLE user_subscription_overrides ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
    ELSE
        RAISE EXCEPTION 'Migration 000130 (down): user_subscription_overrides has a mixed timestamp/timestamptz state that should be unreachable (created_at=%, updated_at=%, expires_at=%) — refusing to silently no-op or silently re-narrow an already-naked column.',
            created_at_type, updated_at_type, expires_at_type;
    END IF;
END $$;

COMMENT ON COLUMN user_subscription_overrides.expires_at IS
    'When this override expires, NULL for permanent overrides.';
COMMENT ON COLUMN user_subscription_overrides.created_at IS NULL;
COMMENT ON COLUMN user_subscription_overrides.updated_at IS NULL;
