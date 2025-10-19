-- Rollback migration: Convert Fly.io IATA codes back to AWS-style codes
-- This reverses the changes made in 000005_migrate_region_codes.up.sql

BEGIN;

-- Step 1: Update default value back to AWS-style
ALTER TABLE users
ALTER COLUMN primary_region SET DEFAULT 'us-east-1';

-- Step 2: Drop the Fly.io constraint
ALTER TABLE users
DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Step 3: Convert user data back to AWS-style codes
UPDATE users
SET primary_region = 'us-east-1'
WHERE primary_region = 'iad';

UPDATE users
SET primary_region = 'eu-west-1'
WHERE primary_region = 'lhr';

UPDATE users
SET primary_region = 'ap-southeast-1'
WHERE primary_region = 'syd';

-- Step 4: Restore old constraint with AWS-style codes
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('us-east-1', 'eu-west-1', 'ap-southeast-1'));

-- Step 5: Restore old column comment
COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (us-east-1 for MVP)';

COMMIT;
