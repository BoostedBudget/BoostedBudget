-- Migration: Create rollover_history table
-- Issue: #36 - Rule Executors - Envelope budgeting rollover tracking
-- Purpose: Track category rollovers for envelope budgeting audit trail
--
-- Rollover Flow:
--   1. Rule executor creates rollover suggestion (TransferSuggestion with rollover action)
--   2. User accepts/rejects suggestion via API
--   3. On acceptance, RolloverHistory record created with status='applied'
--   4. Category allocations are updated (source -= amount, target += amount or to unallocated)
--
-- Rollover Types:
--   - full: Roll over entire remaining amount
--   - capped: Roll over up to a maximum amount (cap_amount in rule action)
--   - percentage: Roll over a percentage of remaining (percentage in rule action)
--
-- Target Options:
--   - target_budget_category_id IS NOT NULL: Funds move to specific category
--   - target_budget_category_id IS NULL: Funds return to budget's unallocated pool

CREATE TABLE rollover_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    budget_id UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    rule_id UUID REFERENCES rules(id) ON DELETE SET NULL,
    rule_execution_id UUID REFERENCES rule_executions(id) ON DELETE SET NULL,
    source_budget_category_id UUID NOT NULL REFERENCES budget_categories(id) ON DELETE CASCADE,
    target_budget_category_id UUID REFERENCES budget_categories(id) ON DELETE SET NULL, -- NULL = to unallocated
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    rollover_type VARCHAR(20) NOT NULL CHECK (rollover_type IN ('full', 'capped', 'percentage')),
    budget_period_start TIMESTAMPTZ NOT NULL,
    budget_period_end TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'rejected')),
    applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Ensure period end is after start
    CONSTRAINT rollover_period_valid CHECK (budget_period_end > budget_period_start)
);

-- Index for user's rollover history (ordered by date, most recent first)
CREATE INDEX idx_rollover_history_user_id ON rollover_history(user_id, created_at DESC);

-- Index for budget-specific rollover history
CREATE INDEX idx_rollover_history_budget_id ON rollover_history(budget_id, created_at DESC);

-- Index for finding pending rollovers
CREATE INDEX idx_rollover_history_status ON rollover_history(status)
    WHERE status = 'pending';

-- Index for finding rollovers by source category
CREATE INDEX idx_rollover_history_source_category ON rollover_history(source_budget_category_id);

-- Add table and column comments
COMMENT ON TABLE rollover_history IS 'Tracks envelope budgeting category rollovers for audit trail';
COMMENT ON COLUMN rollover_history.user_id IS 'User who owns this rollover';
COMMENT ON COLUMN rollover_history.budget_id IS 'Budget containing the categories';
COMMENT ON COLUMN rollover_history.rule_id IS 'Rule that created this rollover (nullable for manual rollovers)';
COMMENT ON COLUMN rollover_history.rule_execution_id IS 'Rule execution that created this rollover';
COMMENT ON COLUMN rollover_history.source_budget_category_id IS 'Category from which funds are rolled over';
COMMENT ON COLUMN rollover_history.target_budget_category_id IS 'Category to receive funds (NULL = unallocated)';
COMMENT ON COLUMN rollover_history.amount IS 'Amount being rolled over (must be positive)';
COMMENT ON COLUMN rollover_history.rollover_type IS 'Type: full (all remaining), capped (up to max), percentage (% of remaining)';
COMMENT ON COLUMN rollover_history.budget_period_start IS 'Start of the budget period from which funds are rolled';
COMMENT ON COLUMN rollover_history.budget_period_end IS 'End of the budget period from which funds are rolled';
COMMENT ON COLUMN rollover_history.status IS 'Status: pending (awaiting user action), applied (executed), rejected (declined)';
COMMENT ON COLUMN rollover_history.applied_at IS 'Timestamp when rollover was applied (if status=applied)';
