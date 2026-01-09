-- Rollback: Remove soft-delete fields from linked_items
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This rollback will:
-- - Remove replaced_at timestamps
-- - Remove replaced_by_id chain references
-- - Restore original status CHECK constraint (removing 'replaced', 'disconnected' values)
-- - Any rows with status='replaced' or 'disconnected' will become invalid!
--
-- Recommended: Before rollback, update any 'replaced' or 'disconnected' items to 'active' or 'revoked'
--
-- Recommended: Only rollback in development or with confirmed database backup

-- =============================================================================
-- Step 1: Drop Indexes (Idempotent)
-- =============================================================================

DROP INDEX IF EXISTS idx_linked_items_institution_status;
DROP INDEX IF EXISTS idx_linked_items_replaced_by;

-- =============================================================================
-- Step 2: Drop Constraints (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Drop the expanded status constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_valid_status'
    ) THEN
        ALTER TABLE linked_items DROP CONSTRAINT linked_items_valid_status;
    END IF;

    -- Drop the replaced_at consistency constraint
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_replaced_at_consistency'
    ) THEN
        ALTER TABLE linked_items DROP CONSTRAINT linked_items_replaced_at_consistency;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Restore Original Status CHECK Constraint (from migration 000013)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_status_check'
    ) THEN
        ALTER TABLE linked_items ADD CONSTRAINT linked_items_status_check
            CHECK (status IN ('active', 'error', 'login_required', 'pending_expiration', 'revoked'));
    END IF;
END $$;

-- =============================================================================
-- Step 4: Drop Columns (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'replaced_by_id'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN replaced_by_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'replaced_at'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN replaced_at;
    END IF;
END $$;

-- Restore original comment
COMMENT ON COLUMN linked_items.status IS 'Connection status: active, error, login_required, pending_expiration, revoked';

-- =============================================================================
-- Rollback Complete
-- =============================================================================
