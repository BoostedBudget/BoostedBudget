-- #972: Add 'savings_goal' to valid_rule_type CHECK constraint on the rules table.
--
-- Migration 000025 created the constraint with only 3 values:
--   ('transfer_suggestion', 'budget_alert', 'category_rollover')
-- The domain constant RuleTypeSavingsGoal = "savings_goal" already exists in Go,
-- but inserting a savings_goal rule fails at the DB level without this fix.
--
-- Strategy: drop the old constraint (if present) and recreate it with all 4 values.
-- Both steps are wrapped in a single DO $$ block for idempotency.

DO $$
BEGIN
    -- Drop the old constraint only if it currently exists.
    -- Using pg_constraint joined to pg_class is more reliable than
    -- information_schema.check_constraints for per-table lookups.
    IF EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_rule_type'
          AND t.relname = 'rules'
    ) THEN
        ALTER TABLE rules DROP CONSTRAINT valid_rule_type;
    END IF;

    -- Re-add the constraint with savings_goal included.
    -- Guard with NOT EXISTS so the block is safe to re-run even if the
    -- first run only partially completed (e.g. dropped but never re-added).
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_rule_type'
          AND t.relname = 'rules'
    ) THEN
        ALTER TABLE rules
            ADD CONSTRAINT valid_rule_type
            CHECK (rule_type IN (
                'transfer_suggestion',
                'budget_alert',
                'category_rollover',
                'savings_goal'
            ));
    END IF;
END $$;
