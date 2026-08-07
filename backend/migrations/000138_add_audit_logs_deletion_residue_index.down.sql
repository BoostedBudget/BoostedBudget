-- Rollback for 000138. Drops only the index; audit_logs and its rows are untouched — the residue
-- records themselves are the durable GDPR trail and are never a migration's to remove. After this,
-- GetDeletionResidueStats still returns the right numbers, by sequential scan.
DROP INDEX CONCURRENTLY IF EXISTS idx_audit_logs_deletion_residue;
