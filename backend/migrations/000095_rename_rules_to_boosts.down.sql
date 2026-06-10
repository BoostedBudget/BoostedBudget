-- #1283: Reverse rename of automation-rule identifiers from "boosts" back to "rules".
-- This is the exact inverse of 000095_rename_rules_to_boosts.up.sql.
-- All steps are guarded with existence checks for idempotency.

-- =============================================================================
-- STEP 1: Restore trigger name
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_trigger t
        JOIN pg_class c ON c.oid = t.tgrelid
        WHERE t.tgname = 'update_boosts_updated_at' AND c.relname = 'boosts'
    ) THEN
        ALTER TRIGGER update_boosts_updated_at ON boosts RENAME TO update_rules_updated_at;
    END IF;
END $$;

-- =============================================================================
-- STEP 2: Restore index names
-- =============================================================================

DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_user_id' AND relkind = 'i') THEN ALTER INDEX idx_boosts_user_id RENAME TO idx_rules_user_id; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_user_enabled_priority' AND relkind = 'i') THEN ALTER INDEX idx_boosts_user_enabled_priority RENAME TO idx_rules_user_enabled_priority; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_type' AND relkind = 'i') THEN ALTER INDEX idx_boosts_type RENAME TO idx_rules_type; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_last_executed' AND relkind = 'i') THEN ALTER INDEX idx_boosts_last_executed RENAME TO idx_rules_last_executed; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_trigger_config' AND relkind = 'i') THEN ALTER INDEX idx_boosts_trigger_config RENAME TO idx_rules_trigger_config; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_conditions' AND relkind = 'i') THEN ALTER INDEX idx_boosts_conditions RENAME TO idx_rules_conditions; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boosts_actions' AND relkind = 'i') THEN ALTER INDEX idx_boosts_actions RENAME TO idx_rules_actions; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boost_executions_boost_id' AND relkind = 'i') THEN ALTER INDEX idx_boost_executions_boost_id RENAME TO idx_rule_executions_rule_id; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boost_executions_started_at' AND relkind = 'i') THEN ALTER INDEX idx_boost_executions_started_at RENAME TO idx_rule_executions_started_at; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boost_executions_status' AND relkind = 'i') THEN ALTER INDEX idx_boost_executions_status RENAME TO idx_rule_executions_status; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boost_executions_failed' AND relkind = 'i') THEN ALTER INDEX idx_boost_executions_failed RENAME TO idx_rule_executions_failed; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_boost_executions_pending' AND relkind = 'i') THEN ALTER INDEX idx_boost_executions_pending RENAME TO idx_rule_executions_pending; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_transfer_sugg_boost_id' AND relkind = 'i') THEN ALTER INDEX idx_transfer_sugg_boost_id RENAME TO idx_transfer_sugg_rule_id; END IF; END $$;

-- =============================================================================
-- STEP 3: Restore constraint names
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'unique_boost_name_per_user' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT unique_boost_name_per_user TO unique_automation_rule_name_per_user;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_boost_type' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT valid_boost_type TO valid_rule_type;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_boosts_condition_logic' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT valid_boosts_condition_logic TO valid_rules_condition_logic;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_boosts_user_id' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT fk_boosts_user_id TO fk_rules_user_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_boost_executions_boost_id' AND t.relname = 'boost_executions'
    ) THEN
        ALTER TABLE boost_executions RENAME CONSTRAINT fk_boost_executions_boost_id TO fk_rule_executions_rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_transfer_suggestions_boost_id' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME CONSTRAINT fk_transfer_suggestions_boost_id TO fk_transfer_suggestions_rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_transfer_suggestions_boost_execution_id' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME CONSTRAINT fk_transfer_suggestions_boost_execution_id TO fk_transfer_suggestions_rule_execution_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rollover_history_boost_id' AND t.relname = 'rollover_history'
    ) THEN
        ALTER TABLE rollover_history RENAME CONSTRAINT fk_rollover_history_boost_id TO fk_rollover_history_rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rollover_history_boost_execution_id' AND t.relname = 'rollover_history'
    ) THEN
        ALTER TABLE rollover_history RENAME CONSTRAINT fk_rollover_history_boost_execution_id TO fk_rollover_history_rule_execution_id;
    END IF;
END $$;

-- Restore source data values BEFORE altering the constraint (non-empty table safety).
-- Must run UPDATE first so existing 'boost' rows don't violate the restored CHECK.
UPDATE transfer_suggestions SET source = 'rule' WHERE source = 'boost';

-- Restore valid_suggestion_source to accept 'rule' instead of 'boost'
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_source' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions DROP CONSTRAINT valid_suggestion_source;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_source' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_source
            CHECK (source IN ('rule', 'manual', 'ai'));
    END IF;
END $$;

-- =============================================================================
-- STEP 4: Restore column names
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollover_history' AND column_name = 'boost_execution_id') THEN
        ALTER TABLE rollover_history RENAME COLUMN boost_execution_id TO rule_execution_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'rollover_history' AND column_name = 'boost_id') THEN
        ALTER TABLE rollover_history RENAME COLUMN boost_id TO rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transfer_suggestions' AND column_name = 'boost_execution_id') THEN
        ALTER TABLE transfer_suggestions RENAME COLUMN boost_execution_id TO rule_execution_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transfer_suggestions' AND column_name = 'boost_id') THEN
        ALTER TABLE transfer_suggestions RENAME COLUMN boost_id TO rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'boost_executions' AND column_name = 'boost_id') THEN
        ALTER TABLE boost_executions RENAME COLUMN boost_id TO rule_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'boosts' AND column_name = 'boost_type') THEN
        ALTER TABLE boosts RENAME COLUMN boost_type TO rule_type;
    END IF;
END $$;

-- =============================================================================
-- STEP 5: Restore table names
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'boost_executions' AND relkind = 'r') THEN
        ALTER TABLE boost_executions RENAME TO rule_executions;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'boosts' AND relkind = 'r') THEN
        ALTER TABLE boosts RENAME TO rules;
    END IF;
END $$;
