-- Rollback 000102: Revert merged_into_merchant_id FK back to ON DELETE SET NULL.
--
-- CAVEAT: This rollback intentionally restores the pre-000102 schema (ON DELETE SET NULL), which
-- reintroduces the latent CHECK-constraint conflict: if a merge-only override row's target merchant
-- is hard-deleted, the SET NULL FK would null the merge pointer, leaving both action columns NULL
-- and violating the CHECK (display_name IS NOT NULL OR merged_into_merchant_id IS NOT NULL).
-- This conflict was the original motivation for 000102. No merchant hard-delete path exists today,
-- so the conflict is latent. Rolling back is correct schema restoration; the risk is documented here.

DO $$
BEGIN
    -- Drop the CASCADE FK if it exists.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_cascade_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            DROP CONSTRAINT merchant_name_overrides_merged_into_cascade_fkey;
    END IF;

    -- Restore the original SET NULL FK.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'merchant_name_overrides_merged_into_merchant_id_fkey'
          AND conrelid = 'merchant_name_overrides'::regclass
    ) THEN
        ALTER TABLE merchant_name_overrides
            ADD CONSTRAINT merchant_name_overrides_merged_into_merchant_id_fkey
            FOREIGN KEY (merged_into_merchant_id)
            REFERENCES merchants(id)
            ON DELETE SET NULL;
    END IF;
END $$;
