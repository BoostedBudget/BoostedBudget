DROP TRIGGER IF EXISTS trg_custom_category_two_levels ON custom_categories;
DROP FUNCTION IF EXISTS enforce_custom_category_two_levels();
ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS custom_category_no_self_parent;
ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS custom_category_single_parent;
ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS fk_custom_categories_parent_id;
ALTER TABLE custom_categories DROP COLUMN IF EXISTS parent_id;
