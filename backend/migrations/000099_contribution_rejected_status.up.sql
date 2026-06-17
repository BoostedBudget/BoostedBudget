-- 000099_contribution_rejected_status.up.sql
--
-- Extends the valid_contribution_status CHECK constraint to include 'rejected'.
--
-- Background (Issue #1300 R3-0 / R3-7):
--   The D6 contribution ledger allows users to reject proposed contributions
--   (detected transfer-ins). A hard DELETE would not be sticky — the next Plaid
--   sync would re-detect the same transfer-in and re-create the proposed row.
--   Storing status='rejected' instead preserves the unique-index tombstone so
--   UpsertProposedFromTransaction's ON CONFLICT … WHERE status='proposed' becomes
--   a no-op for rejected rows, permanently suppressing re-proposal.
--
-- Migration is internally staged and idempotent (DO $$ guards) per C12.
--
-- Up: drop old constraint → add extended constraint.
-- Down (000099…down.sql): drop extended → restore original.

-- =============================================================================
-- 1. Drop the old constraint (only 'proposed','confirmed')
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
-- 2. Add the extended constraint ('proposed','confirmed','rejected')
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_contribution_status'
    ) THEN
        ALTER TABLE savings_goal_contributions
            ADD CONSTRAINT valid_contribution_status
            CHECK (status IN ('proposed', 'confirmed', 'rejected'));
    END IF;
END
$$;
