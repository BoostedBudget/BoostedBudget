-- 000099_contribution_rejected_status.down.sql
--
-- Reverses 000099_contribution_rejected_status.up.sql.
-- Restores the original valid_contribution_status constraint
-- (only 'proposed' and 'confirmed').
--
-- WARNING: Any rows with status='rejected' will violate the restored constraint.
-- If such rows exist, delete or migrate them before running this rollback.
-- All statements are idempotent (DO $$ guards).

-- =============================================================================
-- 1. Drop the extended constraint ('proposed','confirmed','rejected')
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_contribution_status'
    ) THEN
        ALTER TABLE savings_goal_contributions
            DROP CONSTRAINT valid_contribution_status;
    END IF;
END
$$;

-- =============================================================================
-- 2. Restore the original constraint ('proposed','confirmed')
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_contribution_status'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT valid_contribution_status
            CHECK (status IN ('proposed', 'confirmed'));
    END IF;
END
$$;
