-- #1637: shared allocation selectors retain their history when edited.
ALTER TABLE budget_categories ADD COLUMN IF NOT EXISTS group_name text;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_kind;
ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_kind CHECK (category_kind IN ('pfc','custom','savings','group','remainder'));
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_discriminator;
ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_discriminator CHECK (
 (category_kind='pfc' AND pfc_primary IS NOT NULL AND custom_category_id IS NULL AND savings_goal_id IS NULL AND group_name IS NULL)
 OR (category_kind='custom' AND custom_category_id IS NOT NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL AND savings_goal_id IS NULL AND group_name IS NULL)
 OR (category_kind='savings' AND savings_goal_id IS NOT NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL AND custom_category_id IS NULL AND group_name IS NULL)
 OR (category_kind IN ('group','remainder') AND pfc_primary IS NULL AND pfc_detailed IS NULL AND custom_category_id IS NULL AND savings_goal_id IS NULL
     AND match_logic='category_only' AND tags='[]'::jsonb
     AND ((category_kind='group' AND group_name IS NOT NULL AND length(btrim(group_name)) BETWEEN 1 AND 100) OR (category_kind='remainder' AND group_name IS NULL)))
);
DROP INDEX IF EXISTS unique_budget_category_discriminated;
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
 ON budget_categories (budget_id,category_kind,pfc_primary,pfc_detailed,custom_category_id,savings_goal_id)
 NULLS NOT DISTINCT WHERE deleted_at IS NULL AND category_kind <> 'group';

CREATE TABLE IF NOT EXISTS budget_group_members (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 budget_category_id uuid NOT NULL,
 category_kind text NOT NULL,
 pfc_primary text,
 pfc_detailed text,
 custom_category_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 deleted_at timestamptz,
 CONSTRAINT fk_budget_group_members_category FOREIGN KEY (budget_category_id) REFERENCES budget_categories(id) ON DELETE CASCADE,
 CONSTRAINT fk_budget_group_members_custom FOREIGN KEY (custom_category_id) REFERENCES custom_categories(id) ON DELETE CASCADE,
 CONSTRAINT valid_budget_group_member CHECK (
  (category_kind='pfc' AND pfc_primary IS NOT NULL AND custom_category_id IS NULL AND (pfc_detailed IS NULL OR pfc_detailed<>''))
  OR (category_kind='custom' AND custom_category_id IS NOT NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL)
  OR (category_kind='uncategorized' AND custom_category_id IS NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL)
 )
);
CREATE UNIQUE INDEX IF NOT EXISTS unique_live_budget_group_member ON budget_group_members
 (budget_category_id,category_kind,pfc_primary,pfc_detailed,custom_category_id) NULLS NOT DISTINCT WHERE deleted_at IS NULL;
-- Erasure cascades must find retained memberships too, not just live selectors.
CREATE INDEX IF NOT EXISTS idx_budget_group_members_category ON budget_group_members(budget_category_id);
CREATE INDEX IF NOT EXISTS idx_budget_group_members_custom ON budget_group_members(custom_category_id);

-- Same user-scoped hierarchy lock used by the repositories. In particular,
-- budget creation cannot appear after a reparent enumerates its budget universe.
CREATE OR REPLACE FUNCTION serialize_budget_configuration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.user_id::text,0));
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS serialize_budget_creation ON budgets;
CREATE TRIGGER serialize_budget_creation BEFORE INSERT ON budgets
 FOR EACH ROW EXECUTE FUNCTION serialize_budget_configuration();
DROP TRIGGER IF EXISTS serialize_custom_category_creation ON custom_categories;
CREATE TRIGGER serialize_custom_category_creation BEFORE INSERT ON custom_categories
 FOR EACH ROW EXECUTE FUNCTION serialize_budget_configuration();
DROP TRIGGER IF EXISTS serialize_custom_category_hierarchy ON custom_categories;
CREATE TRIGGER serialize_custom_category_hierarchy BEFORE UPDATE OF parent_id,parent_pfc_primary,deleted_at ON custom_categories
 FOR EACH ROW WHEN (OLD.parent_id IS DISTINCT FROM NEW.parent_id OR OLD.parent_pfc_primary IS DISTINCT FROM NEW.parent_pfc_primary OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
 EXECUTE FUNCTION serialize_budget_configuration();

-- All tombstone paths retire memberships, including full-budget deletion and
-- custom-category reassignment. No selector history is destroyed.
CREATE OR REPLACE FUNCTION retire_budget_group_members() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='custom_categories' THEN
  -- Serialize with group edits under the same owning budget locks.
  PERFORM id FROM budgets WHERE user_id=NEW.user_id AND deleted_at IS NULL ORDER BY id FOR UPDATE;
  UPDATE budget_group_members SET deleted_at=now() WHERE custom_category_id=NEW.id AND deleted_at IS NULL;
 ELSIF TG_TABLE_NAME='budgets' THEN
  UPDATE budget_group_members SET deleted_at=now() WHERE deleted_at IS NULL
   AND budget_category_id IN (SELECT id FROM budget_categories WHERE budget_id=NEW.id);
 ELSE
  UPDATE budget_group_members SET deleted_at=now() WHERE budget_category_id=NEW.id AND deleted_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS retire_group_members_on_category ON budget_categories;
CREATE TRIGGER retire_group_members_on_category AFTER UPDATE OF deleted_at ON budget_categories
 FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) EXECUTE FUNCTION retire_budget_group_members();
DROP TRIGGER IF EXISTS retire_group_members_on_custom ON custom_categories;
CREATE TRIGGER retire_group_members_on_custom AFTER UPDATE OF deleted_at ON custom_categories
 FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) EXECUTE FUNCTION retire_budget_group_members();
DROP TRIGGER IF EXISTS retire_group_members_on_budget ON budgets;
CREATE TRIGGER retire_group_members_on_budget AFTER UPDATE OF deleted_at ON budgets
 FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) EXECUTE FUNCTION retire_budget_group_members();
