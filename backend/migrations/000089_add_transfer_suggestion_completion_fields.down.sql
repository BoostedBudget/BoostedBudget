-- Rollback: Remove transfer suggestion completion fields
-- Issue: #972 - Savings transfer automation

-- =============================================================================
-- Drop partial unique index
-- =============================================================================

DROP INDEX IF EXISTS idx_transfer_sugg_matched_txn_id;

-- =============================================================================
-- Drop FK constraint for matched_transaction_id
-- =============================================================================

ALTER TABLE transfer_suggestions DROP CONSTRAINT IF EXISTS fk_transfer_sugg_matched_txn;

-- =============================================================================
-- Remap completed → pending before restoring the old 4-value CHECK constraint.
-- Without this, existing 'completed' rows would violate the constraint being added.
-- =============================================================================

UPDATE transfer_suggestions SET status = 'pending' WHERE status = 'completed';

-- =============================================================================
-- Revert valid_suggestion_status CHECK to original 4 values (no 'completed')
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_status'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions DROP CONSTRAINT valid_suggestion_status;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_status'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_status
            CHECK (status IN ('pending', 'accepted', 'rejected', 'expired'));
    END IF;
END $$;

-- =============================================================================
-- Drop valid_completion_source CHECK constraint
-- =============================================================================

ALTER TABLE transfer_suggestions DROP CONSTRAINT IF EXISTS valid_completion_source;

-- =============================================================================
-- Drop new columns
-- =============================================================================

ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS completion_source;
ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS actual_amount;
ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS matched_transaction_id;
ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS completed_at;
