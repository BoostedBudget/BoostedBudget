-- Rollback for 000107_create_household_members
DROP TRIGGER IF EXISTS update_household_members_updated_at ON household_members;
DROP INDEX IF EXISTS idx_household_members_household_status;
DROP INDEX IF EXISTS uq_household_members_active_owner;
DROP INDEX IF EXISTS uq_household_members_active_user;
DROP TABLE IF EXISTS household_members CASCADE;
