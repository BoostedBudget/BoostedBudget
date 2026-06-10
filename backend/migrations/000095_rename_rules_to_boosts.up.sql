-- #1283: Rename internal automation-rule identifiers to match "Boosts" product branding.
-- This is a metadata-only rename (no row data moved, no index rebuilds).
-- Pre-production / staging data can be wiped; clean break, no compatibility aliases.
--
-- Objects renamed:
--   Tables:       rules → boosts,  rule_executions → boost_executions
--   Columns:      rules.rule_type → boost_type
--                 rule_executions.rule_id → boost_id
--                 transfer_suggestions.rule_id → boost_id
--                 transfer_suggestions.rule_execution_id → boost_execution_id
--                 rollover_history.rule_id → boost_id
--                 rollover_history.rule_execution_id → boost_execution_id
--   Constraints:  see Step 4 below
--   Indexes:      see Step 5 below
--   Triggers:     update_rules_updated_at → update_boosts_updated_at
--   Data:         transfer_suggestions.source 'rule' → 'boost'
--
-- All steps are guarded with existence checks for idempotency.
-- SEPARATE from categorization_rules (Issue #346) which is NOT renamed.

-- =============================================================================
-- STEP 1: Rename tables
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'rules' AND relkind = 'r') THEN
        ALTER TABLE rules RENAME TO boosts;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'rule_executions' AND relkind = 'r') THEN
        ALTER TABLE rule_executions RENAME TO boost_executions;
    END IF;
END $$;

-- =============================================================================
-- STEP 2: Rename columns
-- =============================================================================

-- boosts.rule_type → boost_type
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'boosts' AND column_name = 'rule_type'
    ) THEN
        ALTER TABLE boosts RENAME COLUMN rule_type TO boost_type;
    END IF;
END $$;

-- boost_executions.rule_id → boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'boost_executions' AND column_name = 'rule_id'
    ) THEN
        ALTER TABLE boost_executions RENAME COLUMN rule_id TO boost_id;
    END IF;
END $$;

-- transfer_suggestions.rule_id → boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'rule_id'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME COLUMN rule_id TO boost_id;
    END IF;
END $$;

-- transfer_suggestions.rule_execution_id → boost_execution_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'rule_execution_id'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME COLUMN rule_execution_id TO boost_execution_id;
    END IF;
END $$;

-- rollover_history.rule_id → boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'rollover_history' AND column_name = 'rule_id'
    ) THEN
        ALTER TABLE rollover_history RENAME COLUMN rule_id TO boost_id;
    END IF;
END $$;

-- rollover_history.rule_execution_id → boost_execution_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'rollover_history' AND column_name = 'rule_execution_id'
    ) THEN
        ALTER TABLE rollover_history RENAME COLUMN rule_execution_id TO boost_execution_id;
    END IF;
END $$;

-- =============================================================================
-- STEP 3: Migrate data — transfer_suggestions.source 'rule' → 'boost'
--
-- Do this BEFORE updating the valid_suggestion_source CHECK constraint so that
-- any existing 'rule' rows pass the new constraint after the UPDATE.
-- A fresh schema has no rows, so this is a no-op from scratch.
-- =============================================================================

UPDATE transfer_suggestions SET source = 'boost' WHERE source = 'rule';

-- Drop and recreate valid_suggestion_source to accept 'boost' instead of 'rule'
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_source'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions DROP CONSTRAINT valid_suggestion_source;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_source'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_source
            CHECK (source IN ('boost', 'manual', 'ai'));
    END IF;
END $$;

-- =============================================================================
-- STEP 4: Rename constraints
-- =============================================================================

-- boosts: unique_automation_rule_name_per_user → unique_boost_name_per_user
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'unique_automation_rule_name_per_user' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT unique_automation_rule_name_per_user TO unique_boost_name_per_user;
    END IF;
END $$;

-- boosts: valid_rule_type → valid_boost_type
-- Note: PostgreSQL auto-updates the CHECK expression when the column is renamed above,
-- so the expression now reads boost_type IN (...). We only rename the constraint itself.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_rule_type' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT valid_rule_type TO valid_boost_type;
    END IF;
END $$;

-- boosts: valid_rules_condition_logic → valid_boosts_condition_logic
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_rules_condition_logic' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT valid_rules_condition_logic TO valid_boosts_condition_logic;
    END IF;
END $$;

-- boosts: fk_rules_user_id → fk_boosts_user_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rules_user_id' AND t.relname = 'boosts'
    ) THEN
        ALTER TABLE boosts RENAME CONSTRAINT fk_rules_user_id TO fk_boosts_user_id;
    END IF;
END $$;

-- boost_executions: fk_rule_executions_rule_id → fk_boost_executions_boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rule_executions_rule_id' AND t.relname = 'boost_executions'
    ) THEN
        ALTER TABLE boost_executions RENAME CONSTRAINT fk_rule_executions_rule_id TO fk_boost_executions_boost_id;
    END IF;
END $$;

-- transfer_suggestions: fk_transfer_suggestions_rule_id → fk_transfer_suggestions_boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_transfer_suggestions_rule_id' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME CONSTRAINT fk_transfer_suggestions_rule_id TO fk_transfer_suggestions_boost_id;
    END IF;
END $$;

-- transfer_suggestions: fk_transfer_suggestions_rule_execution_id → fk_transfer_suggestions_boost_execution_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_transfer_suggestions_rule_execution_id' AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions RENAME CONSTRAINT fk_transfer_suggestions_rule_execution_id TO fk_transfer_suggestions_boost_execution_id;
    END IF;
END $$;

-- rollover_history: fk_rollover_history_rule_id → fk_rollover_history_boost_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rollover_history_rule_id' AND t.relname = 'rollover_history'
    ) THEN
        ALTER TABLE rollover_history RENAME CONSTRAINT fk_rollover_history_rule_id TO fk_rollover_history_boost_id;
    END IF;
END $$;

-- rollover_history: fk_rollover_history_rule_execution_id → fk_rollover_history_boost_execution_id
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'fk_rollover_history_rule_execution_id' AND t.relname = 'rollover_history'
    ) THEN
        ALTER TABLE rollover_history RENAME CONSTRAINT fk_rollover_history_rule_execution_id TO fk_rollover_history_boost_execution_id;
    END IF;
END $$;

-- =============================================================================
-- STEP 5: Rename indexes
-- =============================================================================

-- boosts indexes (formerly rules)
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_user_id' AND relkind = 'i') THEN ALTER INDEX idx_rules_user_id RENAME TO idx_boosts_user_id; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_user_enabled_priority' AND relkind = 'i') THEN ALTER INDEX idx_rules_user_enabled_priority RENAME TO idx_boosts_user_enabled_priority; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_type' AND relkind = 'i') THEN ALTER INDEX idx_rules_type RENAME TO idx_boosts_type; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_last_executed' AND relkind = 'i') THEN ALTER INDEX idx_rules_last_executed RENAME TO idx_boosts_last_executed; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_trigger_config' AND relkind = 'i') THEN ALTER INDEX idx_rules_trigger_config RENAME TO idx_boosts_trigger_config; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_conditions' AND relkind = 'i') THEN ALTER INDEX idx_rules_conditions RENAME TO idx_boosts_conditions; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rules_actions' AND relkind = 'i') THEN ALTER INDEX idx_rules_actions RENAME TO idx_boosts_actions; END IF; END $$;

-- boost_executions indexes (formerly rule_executions)
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rule_executions_rule_id' AND relkind = 'i') THEN ALTER INDEX idx_rule_executions_rule_id RENAME TO idx_boost_executions_boost_id; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rule_executions_started_at' AND relkind = 'i') THEN ALTER INDEX idx_rule_executions_started_at RENAME TO idx_boost_executions_started_at; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rule_executions_status' AND relkind = 'i') THEN ALTER INDEX idx_rule_executions_status RENAME TO idx_boost_executions_status; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rule_executions_failed' AND relkind = 'i') THEN ALTER INDEX idx_rule_executions_failed RENAME TO idx_boost_executions_failed; END IF; END $$;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_rule_executions_pending' AND relkind = 'i') THEN ALTER INDEX idx_rule_executions_pending RENAME TO idx_boost_executions_pending; END IF; END $$;

-- transfer_suggestions: idx_transfer_sugg_rule_id → idx_transfer_sugg_boost_id
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_transfer_sugg_rule_id' AND relkind = 'i') THEN ALTER INDEX idx_transfer_sugg_rule_id RENAME TO idx_transfer_sugg_boost_id; END IF; END $$;

-- =============================================================================
-- STEP 6: Rename trigger
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_trigger t
        JOIN pg_class c ON c.oid = t.tgrelid
        WHERE t.tgname = 'update_rules_updated_at' AND c.relname = 'boosts'
    ) THEN
        ALTER TRIGGER update_rules_updated_at ON boosts RENAME TO update_boosts_updated_at;
    END IF;
END $$;

-- =============================================================================
-- STEP 7: Update table/column comments
-- =============================================================================

COMMENT ON TABLE boosts IS 'User-defined automation boosts for transfers, alerts, and rollovers. Subject to subscription tier limits. Issue #34 (renamed from rules in #1283).';
COMMENT ON TABLE boost_executions IS 'Execution history for automation boosts. Used for debugging, auditing, and retry logic. Issue #34 (renamed from rule_executions in #1283).';
COMMENT ON COLUMN boosts.boost_type IS 'Type of automation: transfer_suggestion, budget_alert, category_rollover, savings_goal';
COMMENT ON COLUMN boost_executions.boost_id IS 'Boost that was executed';
COMMENT ON COLUMN transfer_suggestions.boost_id IS 'Boost that created this suggestion (NULL if manual or boost deleted)';
COMMENT ON COLUMN transfer_suggestions.boost_execution_id IS 'Boost execution that created this suggestion (NULL if boost execution deleted)';
COMMENT ON COLUMN rollover_history.boost_id IS 'Boost that created this rollover (nullable for manual rollovers)';
COMMENT ON COLUMN rollover_history.boost_execution_id IS 'Boost execution that created this rollover';
