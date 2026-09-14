DO $$
DECLARE obligations_exist BOOLEAN;
BEGIN
    IF to_regclass('public.posthog_erasure_outbox') IS NOT NULL THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM posthog_erasure_outbox)' INTO obligations_exist;
        IF obligations_exist THEN
            RAISE EXCEPTION 'REFUSING TO DROP posthog_erasure_outbox (REFUSING TO ROLL BACK 000211): durable PostHog erasure obligations or operator audit history exist';
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS posthog_erasure_operator_audit;
DROP TABLE IF EXISTS posthog_erasure_outbox;
