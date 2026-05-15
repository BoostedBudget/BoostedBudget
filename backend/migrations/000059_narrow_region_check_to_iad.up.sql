-- Migration: Narrow region CHECK constraint to iad only
-- lhr and syd were defined but never had real infrastructure.
-- When new regions are added (Phase 7+), a new migration will widen this constraint.

-- Step 1: Migrate any users with non-iad regions to iad (defensive - these regions had no real infra)
UPDATE users SET primary_region = 'iad' WHERE primary_region IN ('lhr', 'syd');

-- Step 2: Drop the old constraint (covers iad, lhr, syd)
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Step 3: Add narrowed constraint (iad only)
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('iad'));

-- Step 4: Update column comment to reflect current state
COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (Fly.io IATA code). See config/regions.yaml for available regions. Multi-region planned for Phase 7+.';
