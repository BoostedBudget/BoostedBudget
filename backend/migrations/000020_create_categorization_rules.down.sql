-- Rollback: Remove categorization rules tables
-- Issue #346: Custom Transaction Categorization Rules

-- =============================================================================
-- PART 1: Drop rule_execution_history table
-- =============================================================================

-- Drop indexes first
DROP INDEX IF EXISTS idx_rule_exec_executed_at;
DROP INDEX IF EXISTS idx_rule_exec_transaction_id;
DROP INDEX IF EXISTS idx_rule_exec_rule_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS rule_execution_history CASCADE;

-- =============================================================================
-- PART 2: Drop categorization_rules table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_categorization_rules_updated_at ON categorization_rules;

-- Drop indexes
DROP INDEX IF EXISTS idx_cat_rules_source;
DROP INDEX IF EXISTS idx_cat_rules_user_id;
DROP INDEX IF EXISTS idx_cat_rules_user_enabled_priority;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS categorization_rules CASCADE;
