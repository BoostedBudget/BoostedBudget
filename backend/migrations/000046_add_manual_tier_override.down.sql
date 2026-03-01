-- Rollback manual tier override columns

ALTER TABLE users DROP COLUMN IF EXISTS manual_tier_override;
ALTER TABLE users DROP COLUMN IF EXISTS manual_tier_ends_at;
