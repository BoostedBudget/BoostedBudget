-- Rollback for 000164: restore the four ON DELETE CASCADE foreign keys and their NOT NULLs, and
-- drop the erasure/snapshot columns this migration added.
--
-- ⚠ DESTRUCTIVE in a way the up migration is not. Restoring NOT NULL requires removing the
-- orphaned rows SET NULL created -- there is no parent left to re-point them at -- so they are
-- deleted here. That is exactly what CASCADE would have done to them in the first place; the
-- information they lost is unrecoverable. Dropping definition_snapshot/rule_name/boost_name
-- likewise discards the only record of what a purged automation was.
--
-- fk_budget_period_events_budget was never changed, so nothing here touches it beyond removing
-- the explanatory comment.
--
-- Idempotent: IF EXISTS guards throughout; the orphan deletes are no-ops once there are no NULLs.

DELETE FROM rollover_history
 WHERE budget_id IS NULL OR source_budget_category_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_budget_id' AND confdeltype <> 'c'
    ) THEN
        ALTER TABLE rollover_history DROP CONSTRAINT fk_rollover_history_budget_id;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_rollover_history_budget_id'
    ) THEN
        ALTER TABLE rollover_history
            ADD CONSTRAINT fk_rollover_history_budget_id
            FOREIGN KEY (budget_id) REFERENCES budgets(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE rollover_history VALIDATE CONSTRAINT fk_rollover_history_budget_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_source_budget_category_id' AND confdeltype <> 'c'
    ) THEN
        ALTER TABLE rollover_history DROP CONSTRAINT fk_rollover_history_source_budget_category_id;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_source_budget_category_id'
    ) THEN
        ALTER TABLE rollover_history
            ADD CONSTRAINT fk_rollover_history_source_budget_category_id
            FOREIGN KEY (source_budget_category_id) REFERENCES budget_categories(id)
            ON DELETE CASCADE NOT VALID;
        ALTER TABLE rollover_history
            VALIDATE CONSTRAINT fk_rollover_history_source_budget_category_id;
    END IF;
END $$;

ALTER TABLE rollover_history ALTER COLUMN budget_id SET NOT NULL;
ALTER TABLE rollover_history ALTER COLUMN source_budget_category_id SET NOT NULL;

DELETE FROM boost_executions WHERE boost_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_boost_executions_boost_id' AND confdeltype <> 'c'
    ) THEN
        ALTER TABLE boost_executions DROP CONSTRAINT fk_boost_executions_boost_id;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_boost_executions_boost_id'
    ) THEN
        ALTER TABLE boost_executions
            ADD CONSTRAINT fk_boost_executions_boost_id
            FOREIGN KEY (boost_id) REFERENCES boosts(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE boost_executions VALIDATE CONSTRAINT fk_boost_executions_boost_id;
    END IF;
END $$;

ALTER TABLE boost_executions ALTER COLUMN boost_id SET NOT NULL;

DROP INDEX IF EXISTS idx_boost_executions_user_id;

ALTER TABLE boost_executions DROP CONSTRAINT IF EXISTS boost_executions_user_id_not_null;
ALTER TABLE boost_executions DROP CONSTRAINT IF EXISTS fk_boost_executions_user_id;
ALTER TABLE boost_executions
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS boost_name,
    DROP COLUMN IF EXISTS definition_snapshot;

DELETE FROM rule_execution_history WHERE rule_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_rule_id' AND confdeltype <> 'c'
    ) THEN
        ALTER TABLE rule_execution_history DROP CONSTRAINT fk_rule_execution_history_rule_id;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_rule_execution_history_rule_id'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT fk_rule_execution_history_rule_id
            FOREIGN KEY (rule_id) REFERENCES categorization_rules(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE rule_execution_history VALIDATE CONSTRAINT fk_rule_execution_history_rule_id;
    END IF;
END $$;

ALTER TABLE rule_execution_history ALTER COLUMN rule_id SET NOT NULL;

DROP INDEX IF EXISTS idx_rule_exec_user_id;

ALTER TABLE rule_execution_history DROP CONSTRAINT IF EXISTS rule_execution_history_user_id_not_null;
ALTER TABLE rule_execution_history DROP CONSTRAINT IF EXISTS fk_rule_execution_history_user_id;
ALTER TABLE rule_execution_history
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS rule_name,
    DROP COLUMN IF EXISTS definition_snapshot;

COMMENT ON CONSTRAINT fk_budget_period_events_budget ON budget_period_events IS NULL;
