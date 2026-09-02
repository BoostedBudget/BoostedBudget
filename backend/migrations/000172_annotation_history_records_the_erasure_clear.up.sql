-- #1569 follow-up (#1590): record the annotation a GDPR erasure destroys on a SURVIVING user.
--
-- WHAT THE FOURTH SOURCE IS FOR. DeleteUserCascade clears dangling custom-category overrides before
-- hard-deleting the erased user, and its WHERE is scoped by CATEGORY OWNER, not transaction owner:
--
--     UPDATE transactions SET custom_category_override_id = NULL, category_override_kind = NULL,
--            effective_category_name = pfc_primary
--     WHERE custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1)
--
-- The clear is REQUIRED and stays: transactions.custom_category_override_id -> custom_categories is
-- ON DELETE RESTRICT, so a surviving user's transaction still pointing at the erased user's
-- category would make the whole `DELETE FROM users` fail. The row it touches, though, can belong to
-- a DIFFERENT, surviving user, and that user's annotation was destroyed with no record of what it
-- said. Every other override-destroying statement in this codebase writes one.
--
-- ⚠ SEVERITY, STATED HONESTLY. This is DEFENCE IN DEPTH, not a live data-loss bug, and the
-- distinction was re-derived rather than inherited. All five writers of
-- transactions.custom_category_override_id are ownership-guarded:
--   * UpdateCategory / BulkUpdateCategory -- services/transaction_service.go refuses a category
--     whose owner is not the caller (added for #1203).
--   * TombstoneByExternalID's settlement move and PullForwardSettledPendingAnnotations (#1586) --
--     both require twin.user_id = s.user_id, so the value can only move within one tenant.
--   * CreateBatch, whose value comes from a categorization rule's target_custom_category_id, which
--     categorization_rule_service.go's assertTargetCategoryOwnedAndLive validates on create, on
--     update and on enable.
-- So the cross-owner row is SCHEMA-reachable and not SERVICE-reachable. Nothing in the DATABASE
-- enforces it, though -- there is no trigger, unlike custom_categories.parent_id's own
-- enforce_custom_category_two_levels -- so a backfill, a manual fix, or a sixth write path that
-- forgets the check reintroduces it silently.
--
-- WHY NOT SOLVE IT BY RETAINING THE ERASED USER'S CATEGORY. Because erasure is a real hard delete
-- (docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md) and no retention decision may leave a row
-- unreachable from `users`. The category goes. What is recorded is the SURVIVING user's loss, on
-- the surviving user's own row.
--
-- ⚠ AND THE RECORD MUST SURVIVE THE ERASURE IT DESCRIBES. transaction_annotation_history.user_id is
-- ON DELETE CASCADE from users (migration 000169), and the recorded user_id is the row's own
-- SURVIVING owner -- inherited from the UPDATE's own RETURNING, never re-derived -- so erasing A
-- cannot take B's record with it, while erasing B still does. Both directions are pinned by
-- TestErasureClear_SurvivingUsersAnnotationIsRecorded_Integration and
-- TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned respectively.
--
-- WHY A FOURTH SOURCE AND NOT 'category'. change_source answers "did the user ask for this". A
-- GDPR erasure of somebody else is the furthest thing from user B asking to recategorise; filing it
-- under 'category' would claim B did something they did not, and the row's whole value is that it
-- explains why B's transaction silently reverted to its raw Plaid category.
--
-- No rows are written for the ERASED user's own transactions: they are about to be hard-deleted in
-- the same transaction, and a record that vanishes with the thing it records is not a record.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review', 'settlement', 'erasure'));
