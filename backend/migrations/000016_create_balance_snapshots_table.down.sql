-- Rollback migration for balance_snapshots table
-- Issue: #353 - Balance Tracking Dashboard

-- Drop table with CASCADE to remove all constraints and dependencies
DROP TABLE IF EXISTS balance_snapshots CASCADE;

-- Indexes will be automatically dropped with the table
