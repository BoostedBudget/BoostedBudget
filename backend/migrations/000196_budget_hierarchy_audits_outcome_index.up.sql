-- Issue #1630 Task B7: index on budget_hierarchy_audits(outcome, examined_at) (companion to
-- migration 000195, mirroring the 000078/000079 split). Created CONCURRENTLY as the sole statement
-- in this file so it is not run inside a transaction.
--
-- "Which budgets did the audit actually flag for a human?" is the first question the admin surface
-- (RegisterAdminBudgetHierarchyAuditOperations) asks this table, and without this index that is a
-- seq scan over one row per budget ever examined.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_budget_hierarchy_audits_outcome
    ON budget_hierarchy_audits (outcome, examined_at);
