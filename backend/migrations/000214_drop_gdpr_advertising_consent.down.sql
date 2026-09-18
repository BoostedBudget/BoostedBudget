-- Restores the column exactly as 000002_create_privacy_settings_table.up.sql
-- defined it (that migration is never edited). Idempotent: ADD COLUMN IF NOT EXISTS.
ALTER TABLE privacy_settings
    ADD COLUMN IF NOT EXISTS gdpr_advertising_consent BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN privacy_settings.gdpr_advertising_consent IS 'Advertising cookies (GDPR consent category)';
