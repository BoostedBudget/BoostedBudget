-- #972: Add lifecycle/completion columns to transfer_suggestions.
--
-- Migration 000025 created valid_suggestion_status with:
--   ('pending', 'accepted', 'rejected', 'expired')
-- This migration adds 'completed' and the supporting columns needed for
-- the savings-transfer automation completion flow.
--
-- New columns:
--   completed_at         TIMESTAMPTZ NULL  — when the suggestion was completed
--   matched_transaction_id UUID NULL       — FK → transactions(id) ON DELETE SET NULL
--   actual_amount        NUMERIC NULL      — amount on the matched transaction (fuzzy confirm)
--   completion_source    TEXT NULL         — 'exact_match' | 'user_confirmed'
--
-- New constraints:
--   valid_completion_source  CHECK(completion_source IN ('exact_match','user_confirmed'))
--   fk_transfer_sugg_matched_txn  FK → transactions(id) ON DELETE SET NULL
--
-- New indexes:
--   idx_transfer_sugg_matched_txn_id  partial unique on matched_transaction_id WHERE NOT NULL
--     (prevents one inflow transaction from completing two suggestions)

-- =============================================================================
-- Step 1: Add completed_at column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'completed_at'
    ) THEN
        ALTER TABLE transfer_suggestions ADD COLUMN completed_at TIMESTAMPTZ NULL;
        COMMENT ON COLUMN transfer_suggestions.completed_at IS
            'Timestamp when the suggestion transitioned to status=completed. '
            'NULL for non-completed suggestions.';
    END IF;
END $$;

-- =============================================================================
-- Step 2: Add matched_transaction_id column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'matched_transaction_id'
    ) THEN
        ALTER TABLE transfer_suggestions ADD COLUMN matched_transaction_id UUID NULL;
        COMMENT ON COLUMN transfer_suggestions.matched_transaction_id IS
            'UUID of the inflow transaction that completed this suggestion. '
            'SET NULL when that transaction is deleted. '
            'Unique (partial index) so one transaction cannot complete two suggestions.';
    END IF;
END $$;

-- =============================================================================
-- Step 3: Add actual_amount column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'actual_amount'
    ) THEN
        ALTER TABLE transfer_suggestions ADD COLUMN actual_amount NUMERIC NULL;
        COMMENT ON COLUMN transfer_suggestions.actual_amount IS
            'Actual transfer amount recorded on the matched transaction. '
            'May differ from the suggested amount in fuzzy-confirmation scenarios.';
    END IF;
END $$;

-- =============================================================================
-- Step 4: Add completion_source column
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transfer_suggestions' AND column_name = 'completion_source'
    ) THEN
        ALTER TABLE transfer_suggestions ADD COLUMN completion_source TEXT NULL;
        COMMENT ON COLUMN transfer_suggestions.completion_source IS
            'How this suggestion was completed: exact_match (automatic) or '
            'user_confirmed (manual confirmation by the user). NULL when not completed.';
    END IF;
END $$;

-- =============================================================================
-- Step 5: Update valid_suggestion_status CHECK to include 'completed'
--
-- Strategy (same pattern as migration 000088): drop the old constraint if
-- present (regardless of which values it currently contains) and recreate it
-- with all 5 values. Both steps are guarded so the block is re-runnable.
-- =============================================================================

DO $$
BEGIN
    -- Drop old constraint if it exists (any form — pre- or post-'completed')
    IF EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_status'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions DROP CONSTRAINT valid_suggestion_status;
    END IF;

    -- Recreate with all 5 valid values
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_suggestion_status'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_status
            CHECK (status IN ('pending', 'accepted', 'rejected', 'expired', 'completed'));
    END IF;
END $$;

-- =============================================================================
-- Step 6: Add valid_completion_source CHECK constraint
-- (nullable column — constraint only fires when non-NULL)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE c.conname = 'valid_completion_source'
          AND t.relname = 'transfer_suggestions'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_completion_source
            CHECK (completion_source IN ('exact_match', 'user_confirmed'));
    END IF;
END $$;

-- =============================================================================
-- Step 7: Add FK constraint for matched_transaction_id → transactions(id)
-- Using NOT VALID + VALIDATE pattern for zero-downtime on existing data.
-- ON DELETE SET NULL preserves the suggestion when the transaction is deleted.
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_transfer_sugg_matched_txn'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT fk_transfer_sugg_matched_txn
            FOREIGN KEY (matched_transaction_id)
            REFERENCES transactions(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;

ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_sugg_matched_txn;

-- =============================================================================
-- Step 8: Partial unique index on matched_transaction_id
-- Prevents one inflow transaction from completing two suggestions.
-- Non-concurrent (plain CREATE UNIQUE INDEX IF NOT EXISTS) to stay safely
-- inside the migration transaction; table is new/low-volume at this stage.
-- =============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS idx_transfer_sugg_matched_txn_id
    ON transfer_suggestions(matched_transaction_id)
    WHERE matched_transaction_id IS NOT NULL;
