-- #1631 M8b: index institution_security_id for the forked-security-identity detection query
-- (repository.SecurityRepository.FindForkedIdentities / CountForkedIdentityGroups).
--
-- Migration 000195 added the column, unindexed, "stored so a future consolidation ... has
-- something to join on". This is that future: detection groups ALL securities by
-- institution_security_id (GROUP BY ... HAVING COUNT(DISTINCT plaid_security_id) > 1), run from
-- /admin/health on every request, so it should not be a sequential scan of the whole table.
--
-- Partial (WHERE institution_security_id IS NOT NULL): the column is NULL for the vast majority
-- of rows (anything Plaid never populated it for), and a NULL institution_security_id can never
-- participate in a fork group — indexing those rows would only add index size for zero query
-- benefit, the same reasoning migration 000134 used for plaid_orphaned_items.
CREATE INDEX IF NOT EXISTS idx_securities_institution_security_id
    ON securities (institution_security_id)
    WHERE institution_security_id IS NOT NULL;
