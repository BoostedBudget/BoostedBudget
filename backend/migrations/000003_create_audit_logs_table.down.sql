-- Rollback: Drop audit_logs table
-- WARNING: This will delete ALL audit trail data (GDPR compliance requirement)
-- Only use in development or with full backup

DROP TABLE IF EXISTS audit_logs CASCADE;
