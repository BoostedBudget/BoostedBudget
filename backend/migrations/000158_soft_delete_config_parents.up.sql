-- Issue #1569 Task 8: stop four config DELETEs from cascading away financial history.
--
-- Before this migration, each of these four statements destroyed a history table with no
-- transaction, no audit row and no guard:
--
--   DELETE FROM savings_goals      -> savings_goal_contributions   (fk_contributions_savings_goal, CASCADE)
--                                  -> budget_categories            (fk_budget_categories_savings_goal, CASCADE)
--                                  -> savings_goal_category_detach (fk_sgcd_savings_goal, CASCADE)
--   DELETE FROM boosts             -> boost_executions             (fk_boost_executions_boost_id, CASCADE)
--   DELETE FROM budgets            -> rollover_history             (fk_rollover_history_budget_id, CASCADE)
--                                  -> budget_period_events         (fk_budget_period_events_budget, CASCADE)
--                                  -> budget_categories            (fk_budget_categories_budget_id, CASCADE)
--   DELETE FROM budget_categories  -> rollover_history             (fk_rollover_history_source_budget_category_id, CASCADE)
--
-- The savings-goal one is the worst: savings_goal_contributions holds money the user logged BY
-- HAND. It is not refetchable from any provider.
--
-- WHY SOFT-DELETING THE PARENT, AND NOT `ON DELETE SET NULL` ON THE CHILD.
-- An earlier draft proposed flipping the history FKs to SET NULL. That is not applicable to a
-- single one of the four, and it was checked against the live schema rather than reasoned about:
--
--   savings_goal_contributions.savings_goal_id      NOT NULL
--   boost_executions.boost_id                       NOT NULL
--   budget_period_events.budget_id                  NOT NULL
--   rollover_history.budget_id                      NOT NULL
--   rollover_history.source_budget_category_id      NOT NULL
--
-- Even where it were applicable it would be worse than the bug for savings goals: every
-- contribution read and ownership probe INNER JOINs savings_goals
-- (savings_contribution_repository.go SumConfirmedByGoal, SumConfirmedByGoalInPeriod, ListByGoal,
-- ConfirmContribution, RejectContribution, DeleteContribution), so an orphaned contribution would
-- be physically present and invisible in every sum, list and confirmation flow. Retention that
-- reads as deletion is not retention.
--
-- So the PARENT is tombstoned instead. The FK graph is left exactly as it is, the child rows keep
-- a live parent to join to, and every read of the four parent tables default-excludes tombstones.
-- Same three-column shape as transactions (migration 000156): WHEN / WHY / WHO.
--
-- GDPR: a tombstoned row is still a row. Every one of these four tables reaches users(id) by
-- ON DELETE CASCADE (savings_goals.user_id, boosts.user_id, budgets.user_id, and
-- budget_categories through budgets), all untouched here, so `DELETE FROM users` still destroys
-- them and their children. Pinned by
-- TestSoftDelete_GDPRErasureStillDestroysTombstonedRows_Integration.
--
-- Idempotent: IF NOT EXISTS on every column and index, pg_constraint guards on every CHECK,
-- IF EXISTS on every drop.

-- =============================================================================
-- Step 1: Tombstone columns on all four parents (idempotent).
-- =============================================================================

ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

ALTER TABLE boosts ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE boosts ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE boosts ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

ALTER TABLE budgets ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE budgets ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE budgets ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

ALTER TABLE budget_categories ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE budget_categories ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE budget_categories ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

-- =============================================================================
-- Step 2: The three columns move together, and the reason vocabulary is closed.
--
-- A timestamp with no reason is a row nothing can explain; a reason with no timestamp is a row no
-- read filter excludes. Mirrors transactions_tombstone_consistency (000156) and
-- accounts_removed_at_consistency (000149).
--
--   user_deleted   -- the user asked for this specific row to go away.
--   parent_deleted -- the row was tombstoned because its parent was. budget_categories only:
--                     a budget's categories, and a goal's auto-created savings categories, both
--                     used to vanish by ON DELETE CASCADE and must keep vanishing.
-- =============================================================================

DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['savings_goals', 'boosts', 'budgets', 'budget_categories'] LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_tombstone_consistency') THEN
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I CHECK (
                     (deleted_at IS NULL     AND deleted_reason IS NULL     AND deleted_by IS NULL)
                  OR (deleted_at IS NOT NULL AND deleted_reason IS NOT NULL AND deleted_by IS NOT NULL)
                 ) NOT VALID', t, t || '_tombstone_consistency');
            EXECUTE format('ALTER TABLE %I VALIDATE CONSTRAINT %I', t, t || '_tombstone_consistency');
        END IF;

        IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_deleted_reason_valid') THEN
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I CHECK (
                     deleted_reason IS NULL
                  OR deleted_reason IN (''user_deleted'', ''parent_deleted'')
                 ) NOT VALID', t, t || '_deleted_reason_valid');
            EXECUTE format('ALTER TABLE %I VALIDATE CONSTRAINT %I', t, t || '_deleted_reason_valid');
        END IF;
    END LOOP;
END $$;

-- =============================================================================
-- Step 3: Re-scope the per-user name uniqueness to LIVE rows only.
--
-- This is not cosmetic. `unique_savings_goal_name_per_user` is a UNIQUE CONSTRAINT over
-- (user_id, name) with no predicate, so the moment a delete stops removing the row, the name stays
-- taken forever: "Emergency Fund" could be created once per account, ever, and the second attempt
-- would surface as ErrSavingsGoalAlreadyExists on a goal the user cannot see. Same for boosts and
-- budgets.
--
-- A UNIQUE CONSTRAINT cannot carry a WHERE clause, so each becomes a partial UNIQUE INDEX of the
-- SAME NAME. The name is load-bearing beyond taste: savings_goal_repository.go, boosts_repository.go
-- and budget_repository.go all map duplicates by
-- `strings.Contains(err.Error(), "unique_<x>_name_per_user")`, and Postgres reports a partial
-- unique index violation under the index name, so reusing the name keeps those mappings working.
-- Pinned by TestSoftDelete_*_NameIsFreedByTombstone_Integration.
--
-- Dropping the constraint drops its implicit index, which is why the CREATE can reuse the name.
-- =============================================================================

ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS unique_savings_goal_name_per_user;
CREATE UNIQUE INDEX IF NOT EXISTS unique_savings_goal_name_per_user
    ON savings_goals (user_id, name) WHERE deleted_at IS NULL;

ALTER TABLE boosts DROP CONSTRAINT IF EXISTS unique_boost_name_per_user;
CREATE UNIQUE INDEX IF NOT EXISTS unique_boost_name_per_user
    ON boosts (user_id, name) WHERE deleted_at IS NULL;

ALTER TABLE budgets DROP CONSTRAINT IF EXISTS unique_budget_name_per_user;
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_name_per_user
    ON budgets (user_id, name) WHERE deleted_at IS NULL;

-- budget_categories' uniqueness is already a partial-capable UNIQUE INDEX
-- (unique_budget_category_discriminated, NULLS NOT DISTINCT), not a constraint. Re-scope it the
-- same way: a tombstoned savings category must not block re-adding the same goal to the budget.
DROP INDEX IF EXISTS unique_budget_category_discriminated;
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
    ON budget_categories (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id, savings_goal_id)
    NULLS NOT DISTINCT
    WHERE deleted_at IS NULL;

-- =============================================================================
-- Step 4: Index the tombstones only, for the same reason 000156 does: live reads filter
-- `deleted_at IS NULL`, which matches nearly every row and is already served by the existing
-- (user_id, ...) indexes with a cheap recheck. The tombstone side is the rare one.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_savings_goals_tombstoned
    ON savings_goals (user_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_boosts_tombstoned
    ON boosts (user_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_budgets_tombstoned
    ON budgets (user_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_budget_categories_tombstoned
    ON budget_categories (budget_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;

COMMENT ON COLUMN savings_goals.deleted_at IS
$COMMENT$When this goal was tombstoned. NULL for every live row; every read default-excludes rows
where this is NOT NULL. Set instead of DELETE so savings_goal_contributions -- money the user
logged by hand -- survives. Purging for real is a second, explicit act
(POST /savings-goals/{id}/purge, refused unless the goal is already tombstoned). Issue #1569.$COMMENT$;

COMMENT ON COLUMN boosts.deleted_at IS
$COMMENT$When this boost was tombstoned. Set instead of DELETE so boost_executions -- the
automation audit trail -- survives. Issue #1569.$COMMENT$;

COMMENT ON COLUMN budgets.deleted_at IS
$COMMENT$When this budget was tombstoned. Set instead of DELETE so rollover_history and
budget_period_events survive. Issue #1569.$COMMENT$;

COMMENT ON COLUMN budget_categories.deleted_at IS
$COMMENT$When this category was tombstoned. Set instead of DELETE so rollover_history rows that
name it as their source survive. deleted_reason = 'parent_deleted' when it was tombstoned because
its budget or its savings goal was. Issue #1569.$COMMENT$;
