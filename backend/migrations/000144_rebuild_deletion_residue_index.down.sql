-- Rollback for 000144. Mirrors 000138's own down exactly: drop only the index; audit_logs and its rows
-- are untouched, because the residue records themselves are the durable GDPR trail and are never a
-- migration's to remove. After this, GetDeletionResidueStats still returns the right numbers, by
-- sequential scan.
--
-- CONCURRENTLY, as the sole statement, for the same transaction-block reason as the up-migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_audit_logs_deletion_residue;
