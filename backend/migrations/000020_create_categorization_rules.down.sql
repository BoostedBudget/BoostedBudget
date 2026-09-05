-- Rollback: Remove categorization rules tables
-- Issue #346: Custom Transaction Categorization Rules
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH for rule_execution_history (#1569 round 12 task 4).
-- That table records what an automation rule did and when; it is still the live, actively-written
-- audit trail today (data_export_repository, custom_category_repository's GDPR-erasure path, and
-- the transaction/rule tombstone tests all read or write it). Same mechanism as
-- 000150/000151/000096/000160/000163/000027/000180: REFUSE while rows exist.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM rule_execution_history;
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000020: rule_execution_history holds % row(s), and this '
            'rollback DROPs the table. It is what an automation rule did and when; nothing '
            'reconstructs it. Export it, or empty it deliberately, before retrying.', n;
    END IF;
END $$;

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
