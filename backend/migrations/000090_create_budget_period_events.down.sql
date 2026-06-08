-- Issue #972: Rollback — drop budget_period_events table
-- Drops the table and all dependent indexes/constraints in one statement.

DROP TABLE IF EXISTS budget_period_events CASCADE;
