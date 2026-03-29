-- Rollback: Remove envelope budgeting schema enhancements
-- Issue #490: Enhanced Budget Schema Migration for Envelope Budgeting

-- =============================================================================
-- PART 1: Remove columns and indexes from budget_categories table
-- =============================================================================

-- Drop GIN index for tags
DROP INDEX IF EXISTS idx_budget_categories_tags;

-- Drop CHECK constraint for match_logic (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_match_logic'
    ) THEN
        ALTER TABLE budget_categories DROP CONSTRAINT valid_match_logic;
    END IF;
END $$;

-- Drop match_logic column (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'match_logic'
    ) THEN
        ALTER TABLE budget_categories DROP COLUMN match_logic;
    END IF;
END $$;

-- Drop tags column (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'tags'
    ) THEN
        ALTER TABLE budget_categories DROP COLUMN tags;
    END IF;
END $$;

-- =============================================================================
-- PART 2: Remove columns and indexes from budgets table
-- =============================================================================

-- Drop GIN index for income_sources
DROP INDEX IF EXISTS idx_budgets_income_sources;

-- Drop income_sources column (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'income_sources'
    ) THEN
        ALTER TABLE budgets DROP COLUMN income_sources;
    END IF;
END $$;

-- Drop income_amount column (idempotent)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'income_amount'
    ) THEN
        ALTER TABLE budgets DROP COLUMN income_amount;
    END IF;
END $$;
