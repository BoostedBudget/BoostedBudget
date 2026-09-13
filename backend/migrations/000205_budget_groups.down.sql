-- Refuse to erase live OR retained grouped-budget configuration.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM budget_categories WHERE category_kind IN ('group','remainder')) THEN
  RAISE EXCEPTION 'Cannot roll back grouped budgets: refusing to roll back 000205: retained group data exists';
 END IF;
 IF to_regclass('budget_group_members') IS NOT NULL THEN
  IF EXISTS (SELECT 1 FROM budget_group_members) THEN
   RAISE EXCEPTION 'Cannot roll back grouped budgets: refusing to roll back 000205: retained group data exists';
  END IF;
 END IF;
END $$;
DROP TRIGGER IF EXISTS retire_group_members_on_category ON budget_categories;
DROP TRIGGER IF EXISTS retire_group_members_on_custom ON custom_categories;
DROP TRIGGER IF EXISTS retire_group_members_on_budget ON budgets;
DROP FUNCTION IF EXISTS retire_budget_group_members();
DROP TRIGGER IF EXISTS serialize_budget_creation ON budgets;
DROP TRIGGER IF EXISTS serialize_custom_category_creation ON custom_categories;
DROP TRIGGER IF EXISTS serialize_custom_category_hierarchy ON custom_categories;
DROP FUNCTION IF EXISTS serialize_budget_configuration();
DROP TABLE IF EXISTS budget_group_members;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_discriminator;
ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_discriminator CHECK (
 (category_kind='pfc' AND pfc_primary IS NOT NULL AND custom_category_id IS NULL AND savings_goal_id IS NULL)
 OR (category_kind='custom' AND custom_category_id IS NOT NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL AND savings_goal_id IS NULL)
 OR (category_kind='savings' AND savings_goal_id IS NOT NULL AND pfc_primary IS NULL AND pfc_detailed IS NULL AND custom_category_id IS NULL)
);
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_kind;
ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_kind CHECK (category_kind IN ('pfc','custom','savings'));
DROP INDEX IF EXISTS unique_budget_category_discriminated;
CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
 ON budget_categories (budget_id,category_kind,pfc_primary,pfc_detailed,custom_category_id,savings_goal_id)
 NULLS NOT DISTINCT WHERE deleted_at IS NULL;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS group_name;
