-- Issue #1685 todo 12, Migration B: widen chk_budget_hierarchy_audits_outcome to admit
-- 'skipped_incomplete_actuals'.
--
-- WHY
-- AuditBudgetHierarchy recomputes every live non-savings category's spent_amount through
-- CalculateCategorySpent. When the budget's ledger is ambiguous or degraded that call returns
-- domain.ErrBudgetActualsIncomplete, and the audit now records the skip as a verdict instead of
-- rolling its own work back and failing the job. The ledger row needs an outcome value the CHECK
-- admits.
--
-- WHY NOT VALID + VALIDATE, NOT A BARE ADD CONSTRAINT
-- Unlike 000195 (whose table was CREATEd empty in the same migration), budget_hierarchy_audits is
-- LIVE: a bare ADD CONSTRAINT would hold ACCESS EXCLUSIVE on it for the whole validation scan,
-- blocking every read of the admin listing and every audit INSERT. ADD CONSTRAINT ... NOT VALID
-- takes that lock only for a catalog write, and VALIDATE CONSTRAINT then performs the scan under
-- SHARE UPDATE EXCLUSIVE, which does not block concurrent SELECT/INSERT/UPDATE/DELETE. A NOT VALID
-- CHECK still rejects NEW rows, so there is no window of weakened enforcement.
--
-- IDEMPOTENT: DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT, and VALIDATE on an already-validated
-- constraint is a no-op.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE budget_hierarchy_audits
    DROP CONSTRAINT IF EXISTS chk_budget_hierarchy_audits_outcome;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE budget_hierarchy_audits
    ADD CONSTRAINT chk_budget_hierarchy_audits_outcome
    CHECK (outcome IN ('repaired', 'verified', 'reported_duplicate_identity', 'reported_escaping_pair', 'skipped_incomplete_actuals'))
    NOT VALID;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE budget_hierarchy_audits
    VALIDATE CONSTRAINT chk_budget_hierarchy_audits_outcome;