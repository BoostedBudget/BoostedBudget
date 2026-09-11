-- Issue #1630 review fix (MED): a budget can carry BOTH a duplicate-parent-identity pair AND an
-- escaping nested pair at once. Before this migration, AuditBudgetHierarchy persisted every
-- flagged category id into ONE typeless union column (flagged_category_ids) and picked a SINGLE
-- preferred `outcome` for the ledger row (duplicate-identity preferred over escaping) — so when
-- both shapes were present, the escaping pair's ids were still stored, but with no record of WHY
-- they were flagged. An operator reading the ledger (or the admin listing built on it) could not
-- tell a duplicate-identity id from an escaping-pair id once both were mixed into one array.
--
-- This migration adds two TYPED arrays alongside flagged_category_ids, so each flagged category id
-- is ALSO stored under the shape check that actually flagged it:
--   - flagged_duplicate_identity_category_ids — tripped domain.DuplicateParentIdentity
--   - flagged_escaping_pair_category_ids      — tripped domain.EscapedNestedChild
-- Both can be non-empty on the SAME row (the combined-finding case this migration exists for);
-- `outcome` still names only the preferred one for filtering, but the evidence for both findings
-- is now always reconstructable from these two columns, regardless of which outcome the row
-- landed on. These two are now what repository.AuditBudgetHierarchy/ListBudgetHierarchyAuditFindings
-- and the admin listing (internal/huma/operations/admin_budget_hierarchy_audits.go) read and
-- expose — flagged_category_ids is left in place, still populated for anyone querying this table
-- directly, but is no longer read by Go code and no longer part of the admin API response.
ALTER TABLE budget_hierarchy_audits
    ADD COLUMN IF NOT EXISTS flagged_duplicate_identity_category_ids UUID[] NOT NULL DEFAULT '{}';

ALTER TABLE budget_hierarchy_audits
    ADD COLUMN IF NOT EXISTS flagged_escaping_pair_category_ids UUID[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN budget_hierarchy_audits.flagged_duplicate_identity_category_ids IS
    'budget_categories.id values that tripped domain.DuplicateParentIdentity against this '
    'budget''s live category set at examination time. Empty unless a duplicate-identity pair was '
    'found — can be non-empty even when outcome is reported_escaping_pair (both shapes present at '
    'once), which is exactly what this column, split out of the untyped flagged_category_ids, '
    'exists to make visible.';

COMMENT ON COLUMN budget_hierarchy_audits.flagged_escaping_pair_category_ids IS
    'budget_categories.id values that tripped domain.EscapedNestedChild against this budget''s '
    'live category set at examination time. Empty unless an escaping nested pair was found — can '
    'be non-empty even when outcome is reported_duplicate_identity (both shapes present at once), '
    'which is exactly what this column, split out of the untyped flagged_category_ids, exists to '
    'make visible.';

COMMENT ON COLUMN budget_hierarchy_audits.flagged_category_ids IS
    'DEPRECATED (Issue #1630 review fix): the untyped union of flagged_duplicate_identity_'
    'category_ids and flagged_escaping_pair_category_ids, kept populated for anything querying '
    'this table directly, but no longer read by application code — it cannot distinguish a '
    'duplicate-identity id from an escaping-pair id when a budget has both findings at once. Use '
    'the two typed columns instead.';
