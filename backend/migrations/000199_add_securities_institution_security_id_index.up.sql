-- #1631 M8b: index institution_security_id for the forked-security-identity detection query
-- (repository.SecurityRepository.FindForkedIdentities / CountForkedIdentityGroups).
--
-- Migration 000195 added the column, unindexed, "stored so a future consolidation ... has
-- something to join on". This is that future: detection groups ALL securities by
-- institution_security_id (GROUP BY institution_security_id HAVING COUNT(*) > 1), run from
-- /admin/health on every request, so it should not be a sequential scan of the whole table.
--
-- ⚠ COUNT(*), not COUNT(DISTINCT plaid_security_id) — the query was corrected to this form
-- (backend/internal/repository/security_repository.go, forkedIdentityGroupsSQL) specifically so
-- this single-column index is COVERING: plaid_security_id carries a UNIQUE constraint
-- (migration 000051), so COUNT(*) and COUNT(DISTINCT plaid_security_id) agree within one
-- institution_security_id group, and the plain form needs nothing from the index but the
-- indexed column itself — an index-only scan, no heap fetch. A DISTINCT-based COUNT would need
-- plaid_security_id in the index too (a covering (institution_security_id, plaid_security_id)
-- composite) to get the same plan; do not reintroduce DISTINCT without widening the index to
-- match, or this comment's claim about the query plan goes stale.
--
-- Partial (WHERE institution_security_id IS NOT NULL): the column is NULL for the vast majority
-- of rows (anything Plaid never populated it for), and a NULL institution_security_id can never
-- participate in a fork group — indexing those rows would only add index size for zero query
-- benefit, the same reasoning migration 000134 used for plaid_orphaned_items.
CREATE INDEX IF NOT EXISTS idx_securities_institution_security_id
    ON securities (institution_security_id)
    WHERE institution_security_id IS NOT NULL;
