-- Rollback: Drop rollover_history table
-- Issue: #36 - Rule Executors

DROP TABLE IF EXISTS rollover_history CASCADE;
