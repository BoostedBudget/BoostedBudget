-- Issue #1569 Task 9: stop deleting a custom category from rewriting the user's history.
--
-- Before this migration, customCategoryRepository.DeleteWithReassignment was a five-step
-- transaction whose first step was pure history destruction:
--
--   UPDATE transactions
--   SET category_override_kind = NULL, custom_category_override_id = NULL,
--       pfc_primary_override = NULL, pfc_detailed_override = NULL,
--       category_source = 'plaid', reviewed = false, reviewed_at = NULL,
--       effective_category_name = pfc_primary
--   WHERE user_id = $1 AND custom_category_override_id = $2
--
-- Every past transaction the user had filed under the category was silently refiled under
-- whatever Plaid had guessed, AND their hand-curated `reviewed` state was wiped. Deleting one
-- row of configuration rewrote an unbounded amount of history, and the UI announced none of it.
--
-- WHY A TOMBSTONE ON THE PARENT, AND NOT `ON DELETE SET NULL` ON transactions.
-- Checked against the live schema rather than reasoned about:
--
--   transactions.custom_category_override_id        -> custom_categories(id) ON DELETE RESTRICT
--   budget_categories.custom_category_id            -> custom_categories(id) ON DELETE CASCADE
--   categorization_rules.target_custom_category_id  -> custom_categories(id) ON DELETE SET NULL
--   custom_categories.parent_id (self)              -> custom_categories(id) ON DELETE RESTRICT
--
-- SET NULL on transactions.custom_category_override_id is not applicable: migration 000065's
-- valid_category_override_discriminator forbids category_override_kind = 'custom' with a NULL
-- custom_category_override_id, so nulling the FK alone trades one constraint violation for
-- another -- and nulling both IS the history rewrite this task removes. And even if it were
-- expressible, effective_category_name (migration 000073, app-maintained, NOT generated) would
-- still have to be rewritten by hand, which is the same destruction under a different name.
--
-- So the PARENT is tombstoned. The FK graph is left exactly as it is:
--   * RESTRICT on transactions never fires again, because nothing DELETEs the row any more.
--     The one remaining hard delete is GDPR erasure, and profile_repository.DeleteUserCascade
--     already clears dangling overrides ahead of it (see its comment at the
--     `clear dangling custom category overrides` UPDATE). Pinned by
--     TestCategoryTombstone_RestrictStillSatisfiable_Integration.
--   * CASCADE on budget_categories never fires again either -- which MATTERS, because task 8
--     (migration 000158) made budget_categories soft-deletable precisely so that
--     rollover_history rows naming them as their source survive. A hard cascade from here would
--     have destroyed exactly what 000158 rescued. DeleteWithReassignment now tombstones the
--     budget categories itself, with deleted_reason = 'parent_deleted', reproducing what the
--     user saw (the budget line disappears) without reproducing what they did not (the rollover
--     trail disappearing too). Pinned by
--     TestCategoryTombstone_BudgetCategoriesTombstonedNotCascaded_Integration.
--   * SET NULL on categorization_rules never fires either; the rules are DISABLED with their
--     target left intact instead. See DeleteWithReassignment's step 2 comment.
--
-- Same three-column shape as transactions (000156) and the four config parents (000158):
-- WHEN / WHY / WHO.
--
-- GDPR: a tombstoned row is still a row. custom_categories.user_id -> users(id) ON DELETE CASCADE
-- is untouched, so erasure still destroys it. Pinned by
-- TestCategoryTombstone_GDPRErasureStillDestroysIt_Integration, which calls
-- profileRepository.DeleteUserCascade rather than a hand-rolled DELETE FROM users.
--
-- Idempotent: IF NOT EXISTS on every column and index, pg_constraint guards on every CHECK,
-- IF EXISTS on every drop.

-- =============================================================================
-- Step 1: Tombstone columns (idempotent).
-- =============================================================================

ALTER TABLE custom_categories ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE custom_categories ADD COLUMN IF NOT EXISTS deleted_reason VARCHAR(40);
ALTER TABLE custom_categories ADD COLUMN IF NOT EXISTS deleted_by VARCHAR(40);

-- =============================================================================
-- Step 2: The three columns move together, and the reason vocabulary is closed.
--
-- A timestamp with no reason is a row nothing can explain; a reason with no timestamp is a row no
-- read filter excludes. Mirrors transactions_tombstone_consistency (000156) and the four
-- *_tombstone_consistency constraints from 000158.
--
-- Only ONE reason is admitted here, unlike 000158's two. 'parent_deleted' is deliberately NOT in
-- the vocabulary: nothing tombstones a custom category because its parent was. Children of a
-- deleted parent are REPARENTED to top-level (DeleteWithReassignment step 3), which is what
-- shipped before this branch and is strictly less destructive than tombstoning them -- a child
-- category is live configuration the user did not ask to delete. Admitting a reason no writer
-- ever writes would make the constraint document a behaviour that does not exist. If a future
-- task decides children should be tombstoned with their parent, it adds the value here, in a
-- migration, deliberately.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'custom_categories_tombstone_consistency'
    ) THEN
        ALTER TABLE custom_categories ADD CONSTRAINT custom_categories_tombstone_consistency
            CHECK (
                (deleted_at IS NULL     AND deleted_reason IS NULL     AND deleted_by IS NULL)
             OR (deleted_at IS NOT NULL AND deleted_reason IS NOT NULL AND deleted_by IS NOT NULL)
            ) NOT VALID;
        ALTER TABLE custom_categories VALIDATE CONSTRAINT custom_categories_tombstone_consistency;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'custom_categories_deleted_reason_valid'
    ) THEN
        ALTER TABLE custom_categories ADD CONSTRAINT custom_categories_deleted_reason_valid
            CHECK (deleted_reason IS NULL OR deleted_reason = 'user_deleted') NOT VALID;
        ALTER TABLE custom_categories VALIDATE CONSTRAINT custom_categories_deleted_reason_valid;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Re-scope the per-user name uniqueness to LIVE rows only.
--
-- Not cosmetic. `unique_user_category_name` is a UNIQUE CONSTRAINT over (user_id, name) with no
-- predicate (migration 000018:91), so the moment a delete stops removing the row, the name is
-- taken forever: a user who deletes "Coffee" could never create "Coffee" again, and the attempt
-- would surface as ErrCategoryAlreadyExists against a category they cannot see.
--
-- A UNIQUE CONSTRAINT cannot carry a WHERE clause, so it becomes a partial UNIQUE INDEX of the
-- SAME NAME. The name is load-bearing beyond taste: custom_category_repository.go's
-- mapCategoryWriteError maps duplicates by `strings.Contains(err.Error(), "unique_user_category_name")`,
-- and Postgres reports a partial unique index violation under the INDEX name, so reusing the name
-- keeps that mapping working. Pinned by
-- TestCategoryTombstone_NameIsFreedByTombstone_Integration and
-- TestCategoryTombstone_DuplicateLiveNameStillMapsToAlreadyExists_Integration.
--
-- Dropping the constraint drops its implicit index; the (user_id, name) lookup index
-- idx_custom_categories_name (migration 000018) is a SEPARATE index and is untouched, so no read
-- path loses its index here.
-- =============================================================================

ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS unique_user_category_name;
CREATE UNIQUE INDEX IF NOT EXISTS unique_user_category_name
    ON custom_categories (user_id, name) WHERE deleted_at IS NULL;

-- =============================================================================
-- Step 4: Index the tombstones only, for the same reason 000156 and 000158 do: live reads filter
-- `deleted_at IS NULL`, which matches nearly every row and is already served by
-- idx_custom_categories_user_id / idx_custom_categories_name with a cheap recheck. The tombstone
-- side is the rare one.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_custom_categories_tombstoned
    ON custom_categories (user_id, deleted_at DESC) WHERE deleted_at IS NOT NULL;

COMMENT ON COLUMN custom_categories.deleted_at IS
$COMMENT$When this category was tombstoned. NULL for every live row; every picker, filter and list
default-excludes rows where this is NOT NULL. Set instead of DELETE so that the transactions filed
under it keep their assignment AND their `reviewed` state, and so the name still resolves on those
historical rows (custom_category_repository.GetNamesByIDs, which is deliberately tombstone-blind).
Issue #1569.$COMMENT$;

COMMENT ON COLUMN custom_categories.deleted_reason IS
$COMMENT$Why the row was tombstoned. Only 'user_deleted' today -- the user asked for this category
to go away. Children are reparented rather than tombstoned, so there is no 'parent_deleted' writer.
Issue #1569.$COMMENT$;

COMMENT ON COLUMN custom_categories.deleted_by IS
$COMMENT$Who tombstoned the row -- a role label ('user'), not a user id. Recorded for the same
reason the delete also writes a custom_category.deleted audit entry: a retention decision with no
recorded cause is not auditable. Issue #1569.$COMMENT$;
