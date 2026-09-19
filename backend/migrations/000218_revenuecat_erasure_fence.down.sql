-- A later rollback refusal cannot restore a fence discarded by this migration.
-- Keep the same empty-authority-only rollback boundary as migration 208.
DO $$
BEGIN
    IF to_regclass('revenuecat_customers') IS NOT NULL THEN
        LOCK TABLE revenuecat_customers IN ACCESS EXCLUSIVE MODE;
        IF EXISTS (SELECT 1 FROM revenuecat_customers) THEN
            RAISE EXCEPTION 'RevenueCat erasure rollback refused while customer data exists';
        END IF;
    END IF;
END $$;
ALTER TABLE IF EXISTS revenuecat_customers DROP COLUMN IF EXISTS erasure_confirmed_at;
ALTER TABLE IF EXISTS revenuecat_customers DROP COLUMN IF EXISTS erasure_started_at;
