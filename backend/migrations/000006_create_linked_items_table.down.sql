-- Drop linked_items table and all associated objects
DROP TRIGGER IF EXISTS update_linked_items_updated_at ON linked_items;
DROP INDEX IF EXISTS idx_linked_items_institution_id;
DROP INDEX IF EXISTS idx_linked_items_item_id;
DROP INDEX IF EXISTS idx_linked_items_user_id;
DROP TABLE IF EXISTS linked_items CASCADE;
