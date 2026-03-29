-- Rollback: Drop privacy_settings table
-- WARNING: This will delete ALL privacy settings and GDPR consent data
-- Only use in development or with full backup

DROP TABLE IF EXISTS privacy_settings CASCADE;
