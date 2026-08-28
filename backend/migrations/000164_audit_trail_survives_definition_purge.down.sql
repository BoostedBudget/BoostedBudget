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
--
-- ⚠⚠ #1569 round 12 task 4 (Codex-cited example): "the information they lost is unrecoverable"
-- was a comment, not a control. Same mechanism as this branch's other guards: REFUSE up front,
-- before any statement below runs, rather than destroy an orphaned row or a definition snapshot
-- that a purged rule/boost left behind on its way out. Two distinct hazards, both checked: (1) the
-- DELETEs below remove rows this migration's SET NULL preserved after a rule/boost/budget was
-- purged -- restoring NOT NULL has no parent left to re-point them at; (2) the column drops
-- discard rule_name/boost_name/definition_snapshot for EVERY row, orphaned or not, which is
-- exactly the information #1569 T7.5's audit trail exists to keep.

DO $$
DECLARE
    n_rollover BIGINT;
    n_boost_orphan BIGINT;
    n_boost_snapshot BIGINT;
    n_rule_orphan BIGINT;
    n_rule_snapshot BIGINT;
BEGIN
    SELECT COUNT(*) INTO n_rollover FROM rollover_history
     WHERE budget_id IS NULL OR source_budget_category_id IS NULL;
    SELECT COUNT(*) INTO n_boost_orphan FROM boost_executions WHERE boost_id IS NULL;
    SELECT COUNT(*) INTO n_boost_snapshot FROM boost_executions
     WHERE boost_name IS NOT NULL OR definition_snapshot IS NOT NULL;
    SELECT COUNT(*) INTO n_rule_orphan FROM rule_execution_history WHERE rule_id IS NULL;
    SELECT COUNT(*) INTO n_rule_snapshot FROM rule_execution_history
     WHERE rule_name IS NOT NULL OR definition_snapshot IS NOT NULL;

    IF n_rollover > 0 OR n_boost_orphan > 0 OR n_boost_snapshot > 0
       OR n_rule_orphan > 0 OR n_rule_snapshot > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000164: % rollover_history orphan row(s) (budget_id or '
            'source_budget_category_id IS NULL), % boost_executions orphan row(s) (boost_id IS '
            'NULL), % boost_executions row(s) carrying a boost_name/definition_snapshot, % '
            'rule_execution_history orphan row(s) (rule_id IS NULL), and % rule_execution_history '
            'row(s) carrying a rule_name/definition_snapshot. This rollback deletes every orphan '
            'row outright (no parent survives to re-point them at) and drops the '
            'name/definition_snapshot columns for ALL rows, orphaned or not -- exactly the audit '
            'trail #1569 T7.5 exists to retain. Export what you need, or delete/clear it yourself '
            'having decided that is acceptable, before retrying.',
            n_rollover, n_boost_orphan, n_boost_snapshot, n_rule_orphan, n_rule_snapshot;
    END IF;
END $$;

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
