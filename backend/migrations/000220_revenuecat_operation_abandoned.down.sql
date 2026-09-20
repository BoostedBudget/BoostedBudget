-- Narrowing the CHECK back would strand or destroy audit rows: refuse while any operation carries
-- the terminal 'abandoned' stage. This mirrors 000217's refusal idiom — an operator must resolve the
-- audit trail deliberately (the value is additive and harmless to leave in place) rather than have a
-- rollback silently rewrite it.
DO $$
BEGIN
    IF to_regclass('revenuecat_operations') IS NOT NULL THEN
        LOCK TABLE revenuecat_operations IN ACCESS EXCLUSIVE MODE;
        IF EXISTS (SELECT 1 FROM revenuecat_operations WHERE stage = 'abandoned') THEN
            RAISE EXCEPTION 'RevenueCat operation rollback refused while abandoned operations exist';
        END IF;
    END IF;
END $$;
ALTER TABLE revenuecat_operations DROP CONSTRAINT IF EXISTS revenuecat_operations_stage_check;
ALTER TABLE revenuecat_operations
    ADD CONSTRAINT revenuecat_operations_stage_check
    CHECK (stage IN ('pending', 'applied', 'superseded')) NOT VALID;
ALTER TABLE revenuecat_operations VALIDATE CONSTRAINT revenuecat_operations_stage_check;
