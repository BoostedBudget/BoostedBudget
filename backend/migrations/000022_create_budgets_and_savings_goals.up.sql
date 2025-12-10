-- Issue #27: Budget Schema & Migrations
-- Create budget tables for budget tracking and savings goals
-- FREE for all subscription tiers (no tier limits)

-- =============================================================================
-- PART 1: Create budgets table
-- =============================================================================

CREATE TABLE IF NOT EXISTS budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,

    -- Budget period configuration
    period VARCHAR(20) NOT NULL,  -- 'weekly', 'monthly', 'quarterly', 'yearly', 'custom'
    start_date DATE NOT NULL,
    end_date DATE,  -- NULL for recurring budgets, set for custom periods

    -- Budget amount
    amount DECIMAL(15,2) NOT NULL,
    currency_code VARCHAR(3) NOT NULL DEFAULT 'USD',  -- ISO 4217

    -- Status tracking
    is_active BOOLEAN NOT NULL DEFAULT true,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_budget_name_per_user UNIQUE (user_id, name)
);

-- Add CHECK constraint for period values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_period'
    ) THEN
        ALTER TABLE budgets
            ADD CONSTRAINT valid_budget_period
            CHECK (period IN ('weekly', 'monthly', 'quarterly', 'yearly', 'custom'));
    END IF;
END $$;

-- Add CHECK constraint for positive amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'positive_budget_amount'
    ) THEN
        ALTER TABLE budgets
            ADD CONSTRAINT positive_budget_amount
            CHECK (amount > 0);
    END IF;
END $$;

-- Performance indexes for budgets
CREATE INDEX IF NOT EXISTS idx_budgets_user_id ON budgets(user_id);
CREATE INDEX IF NOT EXISTS idx_budgets_user_active ON budgets(user_id) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_budgets_created_at ON budgets(created_at);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_budgets_updated_at'
    ) THEN
        CREATE TRIGGER update_budgets_updated_at
            BEFORE UPDATE ON budgets
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 2: Create budget_categories table
-- =============================================================================

CREATE TABLE IF NOT EXISTS budget_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,

    -- Category matching (JSONB array format matching transactions.category)
    -- Example: ["Food & Drink", "Coffee Shops"] or ["Shopping"]
    -- Matches against transaction category OR custom_categories
    category JSONB NOT NULL,

    -- Allocation tracking
    allocated_amount DECIMAL(15,2) NOT NULL,

    -- Denormalized spent amount (updated by app logic when transactions change)
    -- See Issue #451 for sync implementation
    spent_amount DECIMAL(15,2) NOT NULL DEFAULT 0,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints: One category per budget (prevents duplicate category entries)
    CONSTRAINT unique_budget_category UNIQUE (budget_id, category)
);

-- Add CHECK constraint for non-negative allocated amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'non_negative_allocated_amount'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT non_negative_allocated_amount
            CHECK (allocated_amount >= 0);
    END IF;
END $$;

-- Add CHECK constraint for non-negative spent amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'non_negative_spent_amount'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT non_negative_spent_amount
            CHECK (spent_amount >= 0);
    END IF;
END $$;

-- Performance indexes for budget_categories
CREATE INDEX IF NOT EXISTS idx_budget_categories_budget_id ON budget_categories(budget_id);
CREATE INDEX IF NOT EXISTS idx_budget_categories_category ON budget_categories USING gin(category);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_budget_categories_updated_at'
    ) THEN
        CREATE TRIGGER update_budget_categories_updated_at
            BEFORE UPDATE ON budget_categories
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 3: Create savings_goals table
-- =============================================================================

CREATE TABLE IF NOT EXISTS savings_goals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,

    -- Target and progress
    target_amount DECIMAL(15,2) NOT NULL,
    current_amount DECIMAL(15,2) NOT NULL DEFAULT 0,  -- Synced from account balance
    currency_code VARCHAR(3) NOT NULL DEFAULT 'USD',  -- ISO 4217

    -- Required linked account for automatic balance tracking
    -- See Issue #452 for sync implementation
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,

    -- Timeline
    deadline DATE,  -- Optional target date

    -- Status
    is_completed BOOLEAN NOT NULL DEFAULT false,
    completed_at TIMESTAMP WITH TIME ZONE,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_savings_goal_name_per_user UNIQUE (user_id, name)
);

-- Add CHECK constraint for positive target amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'positive_target_amount'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT positive_target_amount
            CHECK (target_amount > 0);
    END IF;
END $$;

-- Add CHECK constraint for non-negative current amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'non_negative_current_amount'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT non_negative_current_amount
            CHECK (current_amount >= 0);
    END IF;
END $$;

-- Performance indexes for savings_goals
CREATE INDEX IF NOT EXISTS idx_savings_goals_user_id ON savings_goals(user_id);
CREATE INDEX IF NOT EXISTS idx_savings_goals_account_id ON savings_goals(account_id);
CREATE INDEX IF NOT EXISTS idx_savings_goals_deadline ON savings_goals(deadline) WHERE deadline IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_savings_goals_active ON savings_goals(user_id) WHERE is_completed = false;
CREATE INDEX IF NOT EXISTS idx_savings_goals_created_at ON savings_goals(created_at);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_savings_goals_updated_at'
    ) THEN
        CREATE TRIGGER update_savings_goals_updated_at
            BEFORE UPDATE ON savings_goals
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 4: Comments for documentation
-- =============================================================================

COMMENT ON TABLE budgets IS 'User-defined budgets for expense tracking. FREE for all subscription tiers. Issue #27.';
COMMENT ON COLUMN budgets.name IS 'Budget name (max 100 chars), unique per user';
COMMENT ON COLUMN budgets.period IS 'Budget period: weekly, monthly, quarterly, yearly, or custom';
COMMENT ON COLUMN budgets.start_date IS 'Budget start date. For recurring budgets, this is the anchor date.';
COMMENT ON COLUMN budgets.end_date IS 'Budget end date. NULL for recurring budgets, set for custom periods.';
COMMENT ON COLUMN budgets.amount IS 'Total budget amount for the period';
COMMENT ON COLUMN budgets.is_active IS 'Whether this budget is actively tracking spending';

COMMENT ON TABLE budget_categories IS 'Category allocations within a budget. Links to transaction categories. Issue #27.';
COMMENT ON COLUMN budget_categories.category IS 'JSONB array matching transaction category format (e.g., ["Food & Drink", "Coffee"])';
COMMENT ON COLUMN budget_categories.allocated_amount IS 'Portion of budget allocated to this category';
COMMENT ON COLUMN budget_categories.spent_amount IS 'Denormalized spent amount, updated when transactions change (Issue #451)';

COMMENT ON TABLE savings_goals IS 'User-defined savings goals with required account linking. FREE for all subscription tiers. Issue #27.';
COMMENT ON COLUMN savings_goals.target_amount IS 'Goal amount to save';
COMMENT ON COLUMN savings_goals.current_amount IS 'Current progress toward goal. Auto-synced from linked account balance (Issue #452).';
COMMENT ON COLUMN savings_goals.account_id IS 'Required: Link to account for automatic balance tracking';
COMMENT ON COLUMN savings_goals.deadline IS 'Optional target date to reach the goal';
COMMENT ON COLUMN savings_goals.is_completed IS 'Whether goal has been achieved';
COMMENT ON COLUMN savings_goals.completed_at IS 'Timestamp when goal was marked complete';
