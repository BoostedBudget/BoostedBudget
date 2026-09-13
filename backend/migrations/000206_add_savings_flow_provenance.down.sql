-- Never destroy newly recorded provenance through a rollback.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public'
               AND table_name = 'savings_goal_contributions' AND column_name = 'flow_kind') THEN
        IF EXISTS (SELECT 1 FROM savings_goal_contributions WHERE flow_kind <> 'legacy_unknown') THEN
            RAISE EXCEPTION 'Cannot remove savings flow provenance while classified contributions exist';
        END IF;
    END IF;
END $$;
ALTER TABLE savings_goal_contributions DROP CONSTRAINT IF EXISTS valid_savings_flow_kind;
-- squawk-ignore ban-drop-column
ALTER TABLE savings_goal_contributions DROP COLUMN IF EXISTS flow_kind;
