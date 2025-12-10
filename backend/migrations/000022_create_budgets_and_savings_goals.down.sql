-- Rollback: Remove budget and savings goal tables
-- Issue #27: Budget Schema & Migrations

-- =============================================================================
-- PART 1: Drop savings_goals table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_savings_goals_updated_at ON savings_goals;

-- Drop indexes
DROP INDEX IF EXISTS idx_savings_goals_created_at;
DROP INDEX IF EXISTS idx_savings_goals_active;
DROP INDEX IF EXISTS idx_savings_goals_deadline;
DROP INDEX IF EXISTS idx_savings_goals_account_id;
DROP INDEX IF EXISTS idx_savings_goals_user_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS savings_goals CASCADE;

-- =============================================================================
-- PART 2: Drop budget_categories table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_budget_categories_updated_at ON budget_categories;

-- Drop indexes
DROP INDEX IF EXISTS idx_budget_categories_category;
DROP INDEX IF EXISTS idx_budget_categories_budget_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS budget_categories CASCADE;

-- =============================================================================
-- PART 3: Drop budgets table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_budgets_updated_at ON budgets;

-- Drop indexes
DROP INDEX IF EXISTS idx_budgets_created_at;
DROP INDEX IF EXISTS idx_budgets_user_active;
DROP INDEX IF EXISTS idx_budgets_user_id;

-- Drop table (CASCADE handles foreign key constraints)
DROP TABLE IF EXISTS budgets CASCADE;
