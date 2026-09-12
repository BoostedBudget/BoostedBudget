-- #1627: distinguish carried goal balances from newly recorded saving activity.
-- Constant default preserves unclassified legacy rows without guessing their origin.
ALTER TABLE savings_goal_contributions
    ADD COLUMN IF NOT EXISTS flow_kind TEXT NOT NULL DEFAULT 'legacy_unknown';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'valid_savings_flow_kind') THEN
        ALTER TABLE savings_goal_contributions ADD CONSTRAINT valid_savings_flow_kind
            CHECK (flow_kind IN ('contribution', 'opening_balance', 'legacy_unknown')) NOT VALID;
    END IF;
END $$;

COMMENT ON COLUMN savings_goal_contributions.flow_kind IS
    'Budget activity provenance, independent of progress basis. opening_balance is carried money, contribution is new recorded activity, legacy_unknown cannot be classified safely. #1627.';
