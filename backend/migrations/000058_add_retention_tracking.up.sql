-- #930: Add retention tracking for 6-month auto-cleanup of inactive/cancelled accounts
ALTER TABLE users ADD COLUMN IF NOT EXISTS retention_started_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS retention_expires_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS retention_notice_30d_sent_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS retention_notice_7d_sent_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_retention_expires ON users (retention_expires_at)
  WHERE retention_expires_at IS NOT NULL AND is_deleted = false AND deletion_requested_at IS NULL;

COMMENT ON COLUMN users.retention_started_at IS '#930: When the 6-month retention period started (set on trial expiry or subscription cancellation)';
COMMENT ON COLUMN users.retention_expires_at IS '#930: When the retention period ends and auto-deletion begins (retention_started_at + 6 months)';
COMMENT ON COLUMN users.retention_notice_30d_sent_at IS '#930: When the 30-day pre-deletion warning email was sent';
COMMENT ON COLUMN users.retention_notice_7d_sent_at IS '#930: When the 7-day pre-deletion warning email was sent';
