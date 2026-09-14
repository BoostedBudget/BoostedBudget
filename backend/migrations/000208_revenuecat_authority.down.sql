-- Never silently discard issued grants, confirmed access or operational audit.
DO $$ DECLARE table_name text; has_data boolean;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['revenuecat_customers','revenuecat_operations','revenuecat_webhooks'] LOOP
        IF to_regclass(table_name) IS NOT NULL THEN
            EXECUTE format('LOCK TABLE %I IN ACCESS EXCLUSIVE MODE', table_name);
            EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I)', table_name) INTO has_data;
            IF has_data THEN
                RAISE EXCEPTION 'RevenueCat authority rollback refused while customer data exists';
            END IF;
        END IF;
    END LOOP;
END $$;
DROP TABLE IF EXISTS revenuecat_webhooks;
DROP TABLE IF EXISTS revenuecat_operations;
DROP TABLE IF EXISTS revenuecat_customers;
