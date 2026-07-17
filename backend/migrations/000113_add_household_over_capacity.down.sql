ALTER TABLE households DROP COLUMN IF EXISTS over_capacity_since;
ALTER TABLE households DROP COLUMN IF EXISTS over_capacity_start_notice_sent_at;
ALTER TABLE households DROP COLUMN IF EXISTS over_capacity_warn3d_sent_at;
