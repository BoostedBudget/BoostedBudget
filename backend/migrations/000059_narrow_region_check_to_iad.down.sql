-- Rollback: Restore broader region CHECK constraint
-- Note: Users migrated to iad in the up migration will remain on iad after rollback.

ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_primary_region;

ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('iad', 'lhr', 'syd'));

COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (Fly.io IATA codes: iad, lhr, syd)';
