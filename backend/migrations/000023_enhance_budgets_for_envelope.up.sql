-- Issue #490: Enhanced Budget Schema Migration for Envelope Budgeting
-- Part of Enhanced Envelope Budgeting epic
-- Adds income tracking to budgets and flexible category matching to budget_categories

-- =============================================================================
-- PART 1: Add income tracking columns to budgets table
-- =============================================================================

-- Add income_amount column (idempotent)
-- Total expected income for this budget period
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'income_amount'
    ) THEN
        ALTER TABLE budgets ADD COLUMN income_amount DECIMAL(15,2) NOT NULL DEFAULT 0;
    END IF;
END $$;

-- Add income_sources column (idempotent)
-- JSONB array of income sources with name, expected_amount, account_id
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budgets' AND column_name = 'income_sources'
    ) THEN
        ALTER TABLE budgets ADD COLUMN income_sources JSONB NOT NULL DEFAULT '[]'::jsonb;
    END IF;
END $$;

-- GIN index for income_sources to enable efficient JSONB queries
CREATE INDEX IF NOT EXISTS idx_budgets_income_sources ON budgets USING gin(income_sources);

-- =============================================================================
-- PART 2: Add tag and match_logic columns to budget_categories table
-- =============================================================================

-- Add tags column (idempotent)
-- JSONB array of transaction tags to match against
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'tags'
    ) THEN
        ALTER TABLE budget_categories ADD COLUMN tags JSONB NOT NULL DEFAULT '[]'::jsonb;
    END IF;
END $$;

-- Add match_logic column (idempotent)
-- Determines how to match transactions: category_only, tag_only, and, or
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'budget_categories' AND column_name = 'match_logic'
    ) THEN
        ALTER TABLE budget_categories ADD COLUMN match_logic VARCHAR(20) NOT NULL DEFAULT 'category_only';
    END IF;
END $$;

-- CHECK constraint for match_logic values (idempotent)
-- Ensures only valid match_logic values are stored
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_match_logic'
    ) THEN
        ALTER TABLE budget_categories ADD CONSTRAINT valid_match_logic
            CHECK (match_logic IN ('category_only', 'tag_only', 'and', 'or'));
    END IF;
END $$;

-- GIN index for tags to enable efficient JSONB queries
CREATE INDEX IF NOT EXISTS idx_budget_categories_tags ON budget_categories USING gin(tags);

-- =============================================================================
-- PART 3: Comments for documentation
-- =============================================================================

COMMENT ON COLUMN budgets.income_amount IS 'Total expected income for this budget period (envelope budgeting). Issue #490.';
COMMENT ON COLUMN budgets.income_sources IS 'JSONB array of income sources with name, expected_amount, account_id. Issue #490.';
COMMENT ON COLUMN budget_categories.tags IS 'JSONB array of transaction tags to match against. Issue #490.';
COMMENT ON COLUMN budget_categories.match_logic IS 'How to match transactions: category_only, tag_only, and, or. Issue #490.';
