-- Rollback audit_logs table creation
-- Migration: 000003_create_audit_logs_table.down.sql

-- Drop indexes
DROP INDEX IF EXISTS idx_audit_logs_resource_history;
DROP INDEX IF EXISTS idx_audit_logs_user_timeline;
DROP INDEX IF EXISTS idx_audit_logs_status;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_resource_id;
DROP INDEX IF EXISTS idx_audit_logs_resource_type;
DROP INDEX IF EXISTS idx_audit_logs_action;
DROP INDEX IF EXISTS idx_audit_logs_user_id;

-- Drop table
DROP TABLE IF EXISTS audit_logs;
