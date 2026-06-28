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
-- Implementation: Idempotent DO $$ block that drops the old FK and re-adds with CASCADE,
-- then verifies the new constraint is in place (fails loudly if FK had a non-standard name).

DO $$
BEGIN
    -- Drop the auto-named FK if it exists.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_merchant_id_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            DROP CONSTRAINT merchant_name_overrides_merged_into_merchant_id_fkey;
    END IF;

    -- Add FK with ON DELETE CASCADE.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            ADD CONSTRAINT merchant_name_overrides_merged_into_cascade_fkey
            FOREIGN KEY (merged_into_merchant_id)
            REFERENCES merchants(id)
            ON DELETE CASCADE;
    END IF;

    -- Verify CASCADE FK is in place; fail if original FK had a non-standard name.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        RAISE EXCEPTION 'Migration 000102: CASCADE FK not installed. '
            'Check pg_constraint for merchant_name_overrides — '
            'original FK may have had a non-standard name.';
    END IF;
END $$;
