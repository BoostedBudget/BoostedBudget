-- Issue #1203: harden budget_categories uniqueness.
-- The old plain UNIQUE constraint permits multiple rows that differ only in NULL
-- discriminator columns (Postgres treats NULLs as distinct), so a user could budget
-- the same PFC primary twice. Replace it with a UNIQUE INDEX using NULLS NOT
-- DISTINCT (PG15+) so NULL discriminators compare equal.

-- Preflight: remove existing duplicates (treating NULLs as equal), keeping one row
-- per discriminated key (the one with the smallest ctid).
DELETE FROM budget_categories a
USING budget_categories b
WHERE a.ctid > b.ctid
  AND a.budget_id = b.budget_id
  AND a.category_kind = b.category_kind
  AND a.pfc_primary IS NOT DISTINCT FROM b.pfc_primary
  AND a.pfc_detailed IS NOT DISTINCT FROM b.pfc_detailed
  AND a.custom_category_id IS NOT DISTINCT FROM b.custom_category_id;

-- Drop the old plain UNIQUE constraint.
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS unique_budget_category_discriminated;

-- Recreate as a UNIQUE INDEX with NULLS NOT DISTINCT.
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
    ON budget_categories (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id)
    NULLS NOT DISTINCT;
