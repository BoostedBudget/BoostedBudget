-- #972: Revert valid_rule_type CHECK constraint to original 3-value set
-- (removes 'savings_goal' that was added by 000088.up.sql).

DO $$
BEGIN
    -- Drop the 4-value constraint if present.
    IF EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_rule_type'
          AND t.relname = 'rules'
    ) THEN
        ALTER TABLE rules DROP CONSTRAINT valid_rule_type;
    END IF;

    -- Restore the original 3-value constraint from migration 000025.
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
                'category_rollover'
            ));
    END IF;
END $$;
