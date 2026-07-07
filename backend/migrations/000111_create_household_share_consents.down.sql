-- Rollback for 000111_create_household_share_consents
-- No updated_at trigger on this table (append-only).
DROP INDEX IF EXISTS idx_household_share_consents_household_user;
DROP TABLE IF EXISTS household_share_consents CASCADE;
