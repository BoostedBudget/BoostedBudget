-- #1569 Task 6 (#1579): record what a category or review-state write OVERWROTE.
--
-- THE DEFECT. Four user-facing paths overwrite a transaction's user-set annotation and keep no
-- record of what was there before:
--
--   * UpdateCategory       (single, PATCH /api/v1/transactions/{id}/category)
--   * BulkUpdateCategory   (bulk,   PATCH /api/v1/transactions/category)
--   * SetReviewed          (both the single PATCH /api/v1/transactions/{id}/review and the bulk
--                           PATCH /api/v1/transactions/review — they share ONE repository method)
--
-- ⚠ #1579 AND THE PLAN BOTH SAY "FOUR PATHS, FOUR METHODS". Re-derived: there are four ENDPOINTS
-- but only THREE repository methods. `SetTransactionReviewed` is a HANDLER
-- (huma/operations/transactions.go), and it calls transactionService.SetReviewed with a
-- one-element slice — there is no single-row review statement to fix separately. Writing the
-- capture into SetReviewed covers the single and bulk review endpoints in one place.
--
-- WHY NOT audit_logs, contrary to the ticket. `audit_logs.changes` is TEXT (not JSONB) and is
-- encrypted at its one existing payload site; `AuditRepository.Log` has no payload parameter at
-- all; and DeleteUserCascade REDACTS it with `SET changes = NULL` — nulling the trail for exactly
-- the users it would exist to evidence. A record that the erasure path empties is not a record.
--
-- WHAT IS AND IS NOT LOST WITHOUT THIS TABLE. The PROVIDER's categorisation is never lost:
-- pfc_primary / pfc_detailed are not written by any of the three statements, so clearing the
-- override always restores it. What is lost is a PREVIOUS USER OVERRIDE replaced by a newer one,
-- and — the fact #1579 missed — the pre-existing `reviewed_at`, which BOTH category statements
-- clobber unconditionally with `reviewed = true, reviewed_at = now`. A transaction the user
-- reviewed in March and recategorised in August reports August, and March is unrecoverable.
--
-- ATOMIC CAPTURE, NOT SELECT-THEN-UPDATE. The writer is a CTE inside each UPDATE's own statement
-- (`UPDATE transactions t ... FROM transactions prior WHERE prior.id = t.id ... RETURNING prior.*`
-- feeding an INSERT), so the prior value is read under the same snapshot and the same row locks as
-- the write that destroys it. A separate SELECT would record a value another writer had already
-- replaced.
--
-- NO FK ON transaction_id, DELIBERATELY. `transactions` is tombstoned rather than deleted since
-- task 7, so an FK would buy nothing on the ordinary path; on the paths that DO hard-delete a
-- transaction (GDPR erasure, the three purges) the user_id CASCADE below already reaches this
-- table. Leaving the column FK-free also means a purge of one transaction cannot silently take the
-- record of what was overwritten on it, which is the direction #1569 always resolves.

CREATE TABLE IF NOT EXISTS transaction_annotation_history (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The row's own path to GDPR erasure, and the key every read filters on. Denormalised at write
    -- time from the UPDATE's own tenancy predicate, never from client input.
    user_id                  UUID NOT NULL,

    -- Which transaction was annotated. No FK: see the header.
    transaction_id           UUID NOT NULL,

    -- 'category' — one of the two recategorisation statements overwrote the annotation.
    -- 'review'   — SetReviewed overwrote it.
    -- The distinction matters because a category write ALSO rewrites reviewed/reviewed_at as a
    -- side effect, and a reader needs to know the user did not ask for that.
    change_source            VARCHAR(16) NOT NULL,

    -- BEFORE. The complete user-set annotation as it was, immediately prior to the write.
    prior_category_override_kind      TEXT,
    prior_pfc_primary_override        TEXT,
    prior_pfc_detailed_override       TEXT,
    prior_custom_category_override_id UUID,
    prior_effective_category_name     TEXT,
    prior_category_source             VARCHAR(20),
    prior_reviewed                    BOOLEAN NOT NULL,
    prior_reviewed_at                 TIMESTAMPTZ,

    -- AFTER. What the same write left behind. Recorded rather than derived: the next row for this
    -- transaction may never arrive, and "before" alone cannot say what replaced it.
    new_category_override_kind        TEXT,
    new_pfc_primary_override          TEXT,
    new_pfc_detailed_override         TEXT,
    new_custom_category_override_id   UUID,
    new_effective_category_name       TEXT,
    new_category_source               VARCHAR(20),
    new_reviewed                      BOOLEAN NOT NULL,
    new_reviewed_at                   TIMESTAMPTZ,

    -- clock_timestamp() at write time, not the transaction start: a bulk recategorisation writes
    -- every row inside one statement, and now() would give them all one timestamp and make the
    -- ORDER BY a coin toss.
    changed_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS fk_transaction_annotation_history_user_id;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT fk_transaction_annotation_history_user_id
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review'));

-- The read path is "this user's annotation history, newest first", paginated. changed_at DESC is
-- the whole ORDER BY, so the index carries it.
CREATE INDEX IF NOT EXISTS idx_transaction_annotation_history_user_changed
    ON transaction_annotation_history (user_id, changed_at DESC);

-- A per-transaction lookup ("what was this one before?") is the other question the table exists to
-- answer, and it is the one a detail view asks.
CREATE INDEX IF NOT EXISTS idx_transaction_annotation_history_transaction
    ON transaction_annotation_history (transaction_id, changed_at DESC);

COMMENT ON TABLE transaction_annotation_history IS
    '#1569 Task 6 (#1579): append-only before/after record of every user-initiated category or '
    'review-state overwrite on a transaction. Written by a CTE inside the UPDATE that destroys the '
    'prior value, so the capture is atomic. Nothing updates or deletes a row here except GDPR '
    'erasure via the user_id CASCADE.';
