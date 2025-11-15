-- Rollback: Revert region codes from Fly.io IATA to AWS-style
-- WARNING: This rollback is DESTRUCTIVE and may be LOSSY
--
-- Data Loss Scenarios:
-- - If users were created AFTER migration with 'iad'/'lhr'/'syd' codes
-- - These users don't have AWS code equivalents in the old system
-- - They will default to 'us-east-1' which may not be their intended region
--
-- Recommended: Only rollback in development or with confirmed database backup

-- Step 1: Revert constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Step 2: Revert data (reverse transformation)
-- Users with 'iad' → 'us-east-1' (US East)
-- Users with 'lhr' → 'eu-west-1' (London/EU West)
-- Users with 'syd' → 'ap-southeast-1' (Sydney/Asia Pacific)
UPDATE users SET primary_region = 'us-east-1' WHERE primary_region = 'iad';
UPDATE users SET primary_region = 'eu-west-1' WHERE primary_region = 'lhr';
UPDATE users SET primary_region = 'ap-southeast-1' WHERE primary_region = 'syd';

-- Step 3: Restore old constraint with AWS codes
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('us-east-1', 'eu-west-1', 'ap-southeast-1'));

-- Step 4: Restore old default value
ALTER TABLE users ALTER COLUMN primary_region SET DEFAULT 'us-east-1';

-- Step 5: Update column comment
COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (AWS-style codes: us-east-1, eu-west-1, ap-southeast-1)';
