DROP INDEX IF EXISTS idx_users_retention_expires;
ALTER TABLE users DROP COLUMN IF EXISTS retention_notice_7d_sent_at;
ALTER TABLE users DROP COLUMN IF EXISTS retention_notice_30d_sent_at;
ALTER TABLE users DROP COLUMN IF EXISTS retention_expires_at;
ALTER TABLE users DROP COLUMN IF EXISTS retention_started_at;
