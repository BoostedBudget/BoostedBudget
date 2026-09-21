-- Issue #1685 todo 12, Migration B rollback: restore the four-value CHECK.
--
-- ⚠ THIS DOWN CANNOT VALIDATE ONCE A ROW CARRIES THE NEW OUTCOME. The constraint is re-added
-- NOT VALID on purpose. NOT VALID still rejects every NEW write of 'skipped_incomplete_actuals', so
-- enforcement for new rows is restored immediately — but it deliberately skips the validation scan,
-- because a single existing row carrying that outcome would make VALIDATE CONSTRAINT fail and block
-- the rollback entirely. The constraint is therefore left unvalidated, a coherent state the
-- application runs against, rather than making the rollback conditional on data.
--
-- IDEMPOTENT: DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT.

ALTER TABLE budget_hierarchy_audits
    DROP CONSTRAINT IF EXISTS chk_budget_hierarchy_audits_outcome;
ALTER TABLE budget_hierarchy_audits
    ADD CONSTRAINT chk_budget_hierarchy_audits_outcome
    CHECK (outcome IN ('repaired', 'verified', 'reported_duplicate_identity', 'reported_escaping_pair'))
    NOT VALID;