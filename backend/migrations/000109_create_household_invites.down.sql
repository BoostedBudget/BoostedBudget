-- Rollback for 000109_create_household_invites
DROP TRIGGER IF EXISTS update_household_invites_updated_at ON household_invites;
DROP INDEX IF EXISTS idx_household_invites_pending_expiry;
DROP INDEX IF EXISTS idx_household_invites_pending_email_blind_idx;
DROP INDEX IF EXISTS uq_household_invites_token_hash;
DROP INDEX IF EXISTS uq_household_invites_pending_email;
DROP TABLE IF EXISTS household_invites CASCADE;
