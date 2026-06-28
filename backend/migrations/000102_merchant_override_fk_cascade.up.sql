-- Migration 000102: Enforce data integrity for merchant override deletions.
-- Changes merchant_name_overrides.merged_into_merchant_id FK from ON DELETE SET NULL
-- to ON DELETE CASCADE (G25).
--
-- Issue: When a merge-only override (display_name IS NULL, merged_into_merchant_id IS NOT NULL)
-- exists and its target is deleted, ON DELETE SET NULL violates the CHECK constraint requiring
-- at least one column to be non-null, blocking normal data cleanup.
--
-- Solution: ON DELETE CASCADE deletes the override row entirely, which is semantically correct
-- since an override pointing to a non-existent merchant is meaningless.
--
-- G30 robustness: the original migration only dropped the expected auto-named FK. This revision
-- drops every FK on merged_into_merchant_id referencing merchants(id), regardless of name, then
-- verifies no SET NULL FK remains. A non-standard old FK name no longer causes a silent miss.

DO $$
DECLARE
    c text;
BEGIN
    -- Drop every FK on merged_into_merchant_id referencing merchants(id), regardless of name.
    -- This handles both the auto-named FK and any non-standard names from earlier migration runs.
    FOR c IN
        SELECT con.conname
        FROM   pg_constraint con
        JOIN   pg_attribute  att
               ON  att.attrelid = con.conrelid
               AND att.attnum   = ANY(con.conkey)
        WHERE  con.conrelid  = 'merchant_name_overrides'::regclass
          AND  con.contype   = 'f'
          AND  con.confrelid = 'merchants'::regclass
          AND  att.attname   = 'merged_into_merchant_id'
    LOOP
        EXECUTE format('ALTER TABLE merchant_name_overrides DROP CONSTRAINT %I', c);
    END LOOP;

    -- Add FK with ON DELETE CASCADE (idempotent).
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname   = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid  = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            ADD CONSTRAINT merchant_name_overrides_merged_into_cascade_fkey
            FOREIGN KEY (merged_into_merchant_id)
            REFERENCES merchants(id)
            ON DELETE CASCADE;
    END IF;

    -- Verify the CASCADE FK is now in place.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname   = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid  = 'merchant_name_overrides'::regclass
    ) THEN
        RAISE EXCEPTION 'Migration 000102: CASCADE FK not installed.';
    END IF;

    -- Verify no SET NULL FK remains on merged_into_merchant_id.
    -- Catches any non-standard old FK name that survived the drop loop above.
    IF EXISTS (
        SELECT 1
        FROM   pg_constraint con
        JOIN   pg_attribute  att
               ON  att.attrelid = con.conrelid
               AND att.attnum   = ANY(con.conkey)
        WHERE  con.conrelid    = 'merchant_name_overrides'::regclass
          AND  con.contype     = 'f'
          AND  con.confrelid   = 'merchants'::regclass
          AND  att.attname     = 'merged_into_merchant_id'
          AND  con.confdeltype = 'n'
    ) THEN
        RAISE EXCEPTION 'Migration 000102: a SET NULL FK on merged_into_merchant_id still exists — data-integrity gap not closed.';
    END IF;
END $$;
