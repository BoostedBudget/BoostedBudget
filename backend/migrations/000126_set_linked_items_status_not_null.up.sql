-- Issue #1444, step 3 of 3: set linked_items.status NOT NULL, then drop the scaffolding CHECK.
--
-- This is the step the whole split exists to make cheap. Since PostgreSQL 12, `SET NOT NULL` looks
-- for an existing VALIDATED CHECK constraint that already proves the column is never NULL and, on
-- finding one, SKIPS the full-table scan entirely — so this takes ACCESS EXCLUSIVE for a catalog
-- write only, not for a scan over every row. 000125 validated exactly such a constraint.
--
-- On PostgreSQL 11 or older this optimisation does not exist and the scan would still happen here.
-- This repo targets PostgreSQL 17 (docker-compose.yml, and CI lints with `squawk --pg-version=17`),
-- so the optimisation is always available.
--
-- The CHECK is dropped immediately afterwards: once the column is NOT NULL the constraint is
-- redundant, and leaving it would mean every future INSERT evaluates the same predicate twice. Both
-- statements are catalog-only, so they are safe to share this one transaction — unlike 000124/000125,
-- which had to be separated precisely because one of them scans.
--
-- Idempotent: both steps are guarded, so a re-run is a no-op.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items'
          AND column_name = 'status'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE linked_items ALTER COLUMN status SET NOT NULL;
    END IF;
END $$;

ALTER TABLE linked_items DROP CONSTRAINT IF EXISTS linked_items_status_not_null;
