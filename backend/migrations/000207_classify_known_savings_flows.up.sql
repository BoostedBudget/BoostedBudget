-- Separate from ADD COLUMN so a backfill does not retain its exclusive schema lock.
-- A transaction link proves this was an allocation, never a manual-basis seed.
-- Unlinked manual rows remain unknown: source/status cannot distinguish a seed.
DO $$ DECLARE updated_count INTEGER;
BEGIN
    LOOP
        WITH batch AS (
            SELECT id FROM savings_goal_contributions
            WHERE flow_kind = 'legacy_unknown' AND transaction_id IS NOT NULL
            ORDER BY id LIMIT 1000
        )
        UPDATE savings_goal_contributions c SET flow_kind = 'contribution'
        FROM batch WHERE c.id = batch.id;
        GET DIAGNOSTICS updated_count = ROW_COUNT;
        EXIT WHEN updated_count = 0;
    END LOOP;
END $$;
ALTER TABLE savings_goal_contributions VALIDATE CONSTRAINT valid_savings_flow_kind;
