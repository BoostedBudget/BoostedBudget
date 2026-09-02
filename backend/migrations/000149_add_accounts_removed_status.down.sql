-- Rollback for 000149: reverse ONLY what the up migration added.
--
-- ⚠ Unlike 000033's down migration, this does NOT drop accounts.status (or any other
-- pre-existing column) — that column, and the retention state it now records, predates this
-- migration and belongs to 000033. Dropping it here would destroy the very retention state
-- #1569 exists to preserve. This down migration only drops removed_at and narrows the CHECK
-- back to the three values that existed before this migration ran.

-- =============================================================================
-- Step 1: Drop the removed_at pairing constraint
-- =============================================================================

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_removed_at_consistency;

-- =============================================================================
-- Step 2: Narrow accounts_valid_status back to ('active', 'replaced', 'expired')
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_valid_status;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_valid_status
            CHECK (status IN ('active', 'replaced', 'expired'));
    END IF;
END $$;

-- =============================================================================
-- Step 3: Drop removed_at column
-- =============================================================================

ALTER TABLE accounts DROP COLUMN IF EXISTS removed_at;
