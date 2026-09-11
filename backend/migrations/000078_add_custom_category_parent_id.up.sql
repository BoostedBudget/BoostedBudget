-- Issue #1203: custom category hierarchy via self-FK.
-- A custom category nests under another CUSTOM primary via parent_id.
-- Mutually exclusive with parent_pfc_primary. This trigger blocks a custom
-- category from nesting under a row that is ITSELF nested via parent_id (no
-- custom-to-custom-to-custom chain) — it does NOT cap total depth at two.
-- A row nested via parent_pfc_primary (a PFC root) may still have a custom
-- child nested under it via parent_id, giving a legal three-level chain
-- (pfc root -> custom child -> custom grandchild), since this trigger only
-- ever inspects the parent's parent_id, never its parent_pfc_primary. See
-- analytics_category_path.go (Issue #1630 / #1626) for the reporting queries
-- that already traverse that third level.
-- Same-user ownership, no-custom-grandchildren, and no-nesting-a-parent are
-- enforced by the trigger below (the bare self-FK only proves the parent row
-- exists).
-- NOTE: the supporting indexes are created CONCURRENTLY in migration 000079
-- (a separate file so they are not wrapped in this migration's transaction).

-- parent_id column (nullable, no default = safe add)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'custom_categories' AND column_name = 'parent_id'
    ) THEN
        ALTER TABLE custom_categories ADD COLUMN parent_id UUID;
    END IF;
END $$;

-- Self-FK. ON DELETE RESTRICT: deleting a parent that still has children is
-- blocked at the DB layer (the service reparents children to top-level first).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_custom_categories_parent_id'
    ) THEN
        ALTER TABLE custom_categories
            ADD CONSTRAINT fk_custom_categories_parent_id
            FOREIGN KEY (parent_id) REFERENCES custom_categories(id)
            ON DELETE RESTRICT NOT VALID;
    END IF;
END $$;
ALTER TABLE custom_categories VALIDATE CONSTRAINT fk_custom_categories_parent_id;

-- Mutual exclusivity: never both parent dimensions; never self-parent.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'custom_category_single_parent'
    ) THEN
        ALTER TABLE custom_categories ADD CONSTRAINT custom_category_single_parent
            CHECK (NOT (parent_pfc_primary IS NOT NULL AND parent_id IS NOT NULL));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'custom_category_no_self_parent'
    ) THEN
        ALTER TABLE custom_categories ADD CONSTRAINT custom_category_no_self_parent
            CHECK (parent_id IS NULL OR parent_id <> id);
    END IF;
END $$;

-- Trigger backstop: same-user, no custom-to-custom-to-custom chain (a parent_id
-- parent must itself be top-level via parent_id — a parent_pfc_primary parent is
-- untouched by this check, see header comment), and a row with children cannot be nested.
CREATE OR REPLACE FUNCTION enforce_custom_category_two_levels()
RETURNS TRIGGER AS $$
DECLARE
    parent_user_id UUID;
    parent_parent_id UUID;
BEGIN
    IF NEW.parent_id IS NOT NULL THEN
        SELECT user_id, parent_id INTO parent_user_id, parent_parent_id
        FROM custom_categories WHERE id = NEW.parent_id;

        IF parent_user_id IS NULL THEN
            RAISE EXCEPTION 'parent category % not found', NEW.parent_id
                USING ERRCODE = 'foreign_key_violation';
        END IF;
        IF parent_user_id <> NEW.user_id THEN
            RAISE EXCEPTION 'parent category must belong to the same user'
                USING ERRCODE = 'check_violation';
        END IF;
        IF parent_parent_id IS NOT NULL THEN
            RAISE EXCEPTION 'parent category must be top-level (no grandchildren)'
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM custom_categories WHERE parent_id = NEW.id) THEN
            RAISE EXCEPTION 'category that has children cannot be nested'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'trg_custom_category_two_levels'
    ) THEN
        CREATE TRIGGER trg_custom_category_two_levels
            BEFORE INSERT OR UPDATE OF parent_id ON custom_categories
            FOR EACH ROW EXECUTE FUNCTION enforce_custom_category_two_levels();
    END IF;
END $$;

COMMENT ON COLUMN custom_categories.parent_id IS
    'Self-FK: nests this custom category under another CUSTOM primary. Mutually exclusive with parent_pfc_primary. Blocks a custom-to-custom-to-custom chain (this parent cannot itself be nested via parent_id); a legal three-level chain still exists via a parent_pfc_primary root (pfc root -> custom child -> custom grandchild).';
