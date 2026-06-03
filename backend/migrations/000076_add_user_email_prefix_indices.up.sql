-- backend/migrations/000076_add_user_email_prefix_indices.up.sql
-- #1178: Prefix blind indices for starts-with admin email search.
-- Stores one row per (user_id, prefix_blind_idx) pair.
-- PRIMARY KEY deduplicates; CASCADE keeps rows consistent with user deletes.

CREATE TABLE IF NOT EXISTS user_email_prefix_indices (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prefix_blind_idx TEXT NOT NULL,
    PRIMARY KEY (user_id, prefix_blind_idx)
);

-- Covering index: answers EXISTS(WHERE prefix_blind_idx = $1 AND user_id = ...)
-- without heap access. Both columns are needed -- include both.
CREATE INDEX IF NOT EXISTS idx_uepi_prefix_blind_idx_user
    ON user_email_prefix_indices(prefix_blind_idx, user_id);
