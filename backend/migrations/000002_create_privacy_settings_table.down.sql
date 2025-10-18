-- Rollback privacy_settings table creation
-- Migration: 000002_create_privacy_settings_table.down.sql

-- Drop trigger
DROP TRIGGER IF EXISTS update_privacy_settings_updated_at ON privacy_settings;

-- Drop indexes
DROP INDEX IF EXISTS idx_privacy_settings_gdpr_consent;
DROP INDEX IF EXISTS idx_privacy_settings_user_id;

-- Drop table
DROP TABLE IF EXISTS privacy_settings;
