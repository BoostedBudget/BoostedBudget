-- Issue #1630 Task B7: the ledger for the budget-hierarchy audit/repair.
--
-- WHY THIS EXISTS
-- domain.DuplicateParentIdentity and domain.EscapedNestedChild (Issue #1630 Task 4 / HIGH-severity
-- review fix) are WRITE-TIME guards only: CreateBudget's batch loop, AddCategoryChecked and
-- UpdateCategoryChecked refuse a NEW row that would collide on parent identity or escape a nested
-- parent's containment envelope, but neither guard is ever run against a row that was persisted
-- before it existed. Any budget already holding one of those shapes — including the production
-- budget issue #1630 names, bf6676b1-7bbd-4f06-b765-62756ce0094e — keeps it forever, invisibly,
-- unless something goes looking. This table is that something's memory.
--
-- WHAT THIS TABLE DOES NOT CLOSE
-- match_logic on an escaping pair is REPORT ONLY (see the outcome vocabulary below) — there is no
-- correct automatic answer for what a user meant a nested category to match, and rewriting it would
-- silently change what their budget matches while destroying the only evidence that could be used
-- to ask them. This ledger makes the shape OBSERVABLE; it does not fix Issue #1630's own remaining
-- limitation (ErrEscapedNestedChild's doc comment in domain/budget.go) for any row that predates the
-- guard.
--
-- WHY REFUSALS-TO-REWRITE ARE ROWS, NOT SILENCE
-- Mirrors portfolio_snapshot_repairs (migration 000163, #1569 Task 11): a decision to leave
-- something alone is only auditable if it is recorded as a decision. 'reported_duplicate_identity'
-- and 'reported_escaping_pair' are the refusals here, parallel to that table's
-- refused_no_coverage/refused_partial_coverage.
--
-- IDEMPOTENCE
-- One row per budget_id examined, ever. Once a budget has a row here it is never re-examined by the
-- periodic sweep (repository.BudgetIDsWithUnexaminedHierarchy excludes it); a forced re-run of
-- repository.budgetRepository.AuditBudgetHierarchy against the same budget is itself safe (the
-- spent_amount correction is guarded by IS DISTINCT FROM and the ledger INSERT is ON CONFLICT DO
-- NOTHING) but will not produce a second ledger row.

CREATE TABLE IF NOT EXISTS budget_hierarchy_audits (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id               UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- repaired                      — no duplicate-identity or escaping-pair shape found; at least
    --                                 one category's spent_amount disagreed with the narrow
    --                                 recomputation and was corrected.
    -- verified                      — no shape found and every category's spent_amount already
    --                                 matched; nothing written.
    -- reported_duplicate_identity   — this budget has a live duplicate-parent-identity pair
    --                                 (domain.DuplicateParentIdentity). Identity fields are never
    --                                 touched; spent_amount corrections (if any) still apply.
    -- reported_escaping_pair        — this budget has a live escaping nested pair
    --                                 (domain.EscapedNestedChild). match_logic is never touched;
    --                                 spent_amount corrections (if any) still apply.
    outcome                 VARCHAR(40) NOT NULL,
    reason                  TEXT NOT NULL,

    categories_examined     INTEGER NOT NULL,
    categories_repaired     INTEGER NOT NULL,

    -- The budget_categories.id values that tripped a shape check (duplicate identity or escaping
    -- containment), for a 'reported_*' outcome — empty for 'repaired'/'verified'. This is the
    -- evidence an operator needs to find the actual rows without re-deriving the check by hand.
    flagged_category_ids    UUID[] NOT NULL DEFAULT '{}',

    examined_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- squawk-ignore prefer-robust-stmts
ALTER TABLE budget_hierarchy_audits
    DROP CONSTRAINT IF EXISTS chk_budget_hierarchy_audits_outcome;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE budget_hierarchy_audits
    ADD CONSTRAINT chk_budget_hierarchy_audits_outcome
    CHECK (outcome IN ('repaired', 'verified', 'reported_duplicate_identity', 'reported_escaping_pair'));

-- One examination per budget, ever: this is what makes the audit idempotent and skippable, and what
-- the ledger INSERT's ON CONFLICT (budget_id) DO NOTHING relies on. Not CONCURRENTLY: the table is
-- CREATEd in this same migration and is therefore guaranteed empty and unreferenced here, so there
-- is no lock to avoid and CONCURRENTLY cannot run in a transaction.
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX IF NOT EXISTS uq_budget_hierarchy_audits_budget_id
    ON budget_hierarchy_audits (budget_id);

-- The secondary lookup index (outcome, examined_at) is NOT created here — see migration 000196.
-- Unlike the unique index above, it is not needed for this migration's own constraints to make
-- sense, so it is split into its own CONCURRENTLY migration rather than riding along non-concurrent
-- just because the table happens to be empty right now (the 000078/000079 split is the local
-- pattern for exactly this reason).

COMMENT ON TABLE budget_hierarchy_audits IS
    'One row per budget the #1630 Task B7 audit EXAMINED — repaired, verified, or reported. A '
    '''reported_*'' row is a shape (duplicate parent identity, or an escaping nested pair) the '
    'write-time guards refuse going forward but cannot retroactively fix; spent_amount corrections '
    'still apply for those rows, match_logic and identity fields never do.';

COMMENT ON COLUMN budget_hierarchy_audits.flagged_category_ids IS
    'budget_categories.id values that tripped domain.DuplicateParentIdentity or '
    'domain.EscapedNestedChild against this budget''s live category set at examination time. Empty '
    'for repaired/verified outcomes.';
