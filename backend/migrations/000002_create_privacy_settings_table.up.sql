-- Create privacy_settings table for GDPR compliance
-- Migration: 000002_create_privacy_settings_table.up.sql

CREATE TABLE IF NOT EXISTS privacy_settings (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign key to users table
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- GDPR Cookie Consent
    gdpr_necessary_consent BOOLEAN NOT NULL DEFAULT TRUE,
    -- Always true - required for app to function

    gdpr_analytics_consent BOOLEAN NOT NULL DEFAULT FALSE,
    -- Google Analytics, usage tracking

    gdpr_advertising_consent BOOLEAN NOT NULL DEFAULT FALSE,
    -- Google AdSense (free tier only)

    gdpr_consent_timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    -- When user last updated consent preferences

    -- Email preferences
    marketing_emails_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    product_updates_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    security_alerts_enabled BOOLEAN NOT NULL DEFAULT TRUE,

    -- Data sharing preferences
    data_sharing_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- Share anonymized data for research/improvement

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Ensure one privacy settings record per user
    CONSTRAINT unique_user_privacy_settings UNIQUE(user_id)
);

-- Indexes
CREATE INDEX idx_privacy_settings_user_id ON privacy_settings(user_id);
CREATE INDEX idx_privacy_settings_gdpr_consent ON privacy_settings(gdpr_consent_timestamp);

-- Trigger to automatically update updated_at
CREATE TRIGGER update_privacy_settings_updated_at
    BEFORE UPDATE ON privacy_settings
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments
COMMENT ON TABLE privacy_settings IS 'GDPR-compliant user privacy and consent preferences';
COMMENT ON COLUMN privacy_settings.gdpr_necessary_consent IS 'Required cookies - always true';
COMMENT ON COLUMN privacy_settings.gdpr_analytics_consent IS 'Analytics cookies (Google Analytics)';
COMMENT ON COLUMN privacy_settings.gdpr_advertising_consent IS 'Advertising cookies (AdSense for free tier)';
