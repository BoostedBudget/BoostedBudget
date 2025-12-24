-- Rollback: Remove automation rules schema
-- Issue #34: Rules Schema & Migrations

-- =============================================================================
-- PART 1: Drop transfer_suggestions table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_transfer_suggestions_updated_at ON transfer_suggestions;

-- Drop indexes
DROP INDEX IF EXISTS idx_transfer_sugg_pending;
DROP INDEX IF EXISTS idx_transfer_sugg_created_at;
DROP INDEX IF EXISTS idx_transfer_sugg_expires_at;
DROP INDEX IF EXISTS idx_transfer_sugg_status;
DROP INDEX IF EXISTS idx_transfer_sugg_rule_id;
DROP INDEX IF EXISTS idx_transfer_sugg_user_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS transfer_suggestions CASCADE;

-- =============================================================================
-- PART 2: Drop rule_executions table
-- =============================================================================

-- Drop indexes
DROP INDEX IF EXISTS idx_rule_executions_pending;
DROP INDEX IF EXISTS idx_rule_executions_failed;
DROP INDEX IF EXISTS idx_rule_executions_status;
DROP INDEX IF EXISTS idx_rule_executions_started_at;
DROP INDEX IF EXISTS idx_rule_executions_rule_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS rule_executions CASCADE;

-- =============================================================================
-- PART 3: Drop rules table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_rules_updated_at ON rules;

-- Drop indexes
DROP INDEX IF EXISTS idx_rules_actions;
DROP INDEX IF EXISTS idx_rules_conditions;
DROP INDEX IF EXISTS idx_rules_trigger_config;
DROP INDEX IF EXISTS idx_rules_last_executed;
DROP INDEX IF EXISTS idx_rules_type;
DROP INDEX IF EXISTS idx_rules_user_enabled_priority;
DROP INDEX IF EXISTS idx_rules_user_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS rules CASCADE;
