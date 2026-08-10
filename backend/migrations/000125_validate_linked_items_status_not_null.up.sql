-- Issue #1444, step 2 of 3: validate the CHECK added by 000124.
--
-- This is the step that performs the full-table scan — and the reason the transition is split at
-- all. VALIDATE CONSTRAINT takes only SHARE UPDATE EXCLUSIVE, which does NOT block concurrent
-- SELECTs, INSERTs, UPDATEs or DELETEs on linked_items. Contrast a bare `SET NOT NULL`, which
-- performs the same scan under ACCESS EXCLUSIVE and blocks all of them. See 000124's header for the
-- full three-step rationale.
--
-- If any NULL row somehow survived 000123's backfill, THIS is where the deploy fails — loudly, and
-- without having taken a blocking lock or altered the column. That is the intended fail-closed
-- behaviour: a failure here leaves the column nullable and the constraint un-validated, which is a
-- coherent state the application still runs against.
--
-- Idempotent: validating an already-validated constraint is a no-op in PostgreSQL, but this is
-- guarded on pg_constraint.convalidated anyway so a re-run does not even attempt the scan.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'linked_items'::regclass
          AND conname = 'linked_items_status_not_null'
          AND NOT convalidated
    ) THEN
        ALTER TABLE linked_items VALIDATE CONSTRAINT linked_items_status_not_null;
    END IF;
END $$;
