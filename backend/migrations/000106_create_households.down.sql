-- Rollback for 000106_create_households
DROP TRIGGER IF EXISTS update_households_updated_at ON households;
DROP TABLE IF EXISTS households CASCADE;
