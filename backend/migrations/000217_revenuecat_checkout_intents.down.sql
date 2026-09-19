DO $$
BEGIN
    IF to_regclass('revenuecat_checkout_intents') IS NOT NULL THEN
        LOCK TABLE revenuecat_checkout_intents IN ACCESS EXCLUSIVE MODE;
        IF EXISTS (SELECT 1 FROM revenuecat_checkout_intents) THEN
            RAISE EXCEPTION 'Checkout intent rollback refused while records exist';
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS revenuecat_checkout_intents;
