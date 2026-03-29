-- Migration to convert region codes from AWS-style to Fly.io IATA codes
-- Issue #104: Align region codes with Fly.io deployment regions
--
-- AWS-style codes -> Fly.io IATA codes:
-- us-east-1        -> iad (Virginia)
-- eu-west-1        -> lhr (London)
-- ap-southeast-1   -> syd (Sydney)
--
-- Note: golang-migrate handles transaction management, no explicit BEGIN/COMMIT needed

-- Step 1: Update existing user data
-- Convert AWS-style region codes to Fly.io IATA codes
UPDATE users
SET primary_region = 'iad'
WHERE primary_region = 'us-east-1';

UPDATE users
SET primary_region = 'lhr'
WHERE primary_region = 'eu-west-1';

UPDATE users
SET primary_region = 'syd'
WHERE primary_region = 'ap-southeast-1';

-- Step 2: Drop the old region constraint
ALTER TABLE users
DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Step 3: Add new constraint with Fly.io IATA codes
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('iad', 'lhr', 'syd'));

-- Step 4: Update the default value for new users
ALTER TABLE users
ALTER COLUMN primary_region SET DEFAULT 'iad';

-- Step 5: Update column comment to reflect new codes
COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (Fly.io IATA codes: iad, lhr, syd)';
