-- Migration: Add Soft-Delete Fields to linked_items
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Enable soft-delete for linked_items to preserve historical data on re-link
--
-- When a user re-links a bank account:
-- 1. The old linked_item is marked as 'replaced' (not deleted)
-- 2. A new linked_item is created with the new Plaid item_id
-- 3. replaced_by_id links old to new for tracking the replacement chain
--
-- Expands existing status values (from migration 000013) with:
-- - 'replaced': Was replaced by a re-link, data preserved but read-only
-- - 'disconnected': User manually disconnected, data preserved
--
-- Existing status values (unchanged):
-- - 'active': Currently syncing, primary item for this institution
-- - 'error': General connection error
-- - 'login_required': User needs to re-authenticate
-- - 'pending_expiration': Item access expiring soon
-- - 'revoked': User revoked access at their bank

-- =============================================================================
-- Step 1: Add Soft-Delete Columns (Idempotent)
-- =============================================================================

DO $$
BEGIN
    -- Add replaced_at timestamp (when this item was replaced)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'replaced_at'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN replaced_at TIMESTAMPTZ;
    END IF;

    -- Add replaced_by_id self-reference (points to the replacement item)
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'replaced_by_id'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN replaced_by_id UUID REFERENCES linked_items(id);
    END IF;
END $$;

-- =============================================================================
-- Step 2: Update Status CHECK Constraint to Include New Values (Idempotent)
-- =============================================================================

-- Drop the old check constraint if it exists (from migration 000013)
DO $$
BEGIN
    -- Check if the old constraint exists and drop it
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_status_check'
    ) THEN
        ALTER TABLE linked_items DROP CONSTRAINT linked_items_status_check;
    END IF;
END $$;

-- Add new check constraint with expanded values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_valid_status'
    ) THEN
        ALTER TABLE linked_items ADD CONSTRAINT linked_items_valid_status
            CHECK (status IN (
                'active', 'error', 'login_required', 'pending_expiration', 'revoked',
                'replaced', 'disconnected'
            ));
    END IF;
END $$;

-- =============================================================================
-- Step 3: Add Consistency Constraint (Idempotent)
-- =============================================================================

-- Check constraint: replaced_at must be set when status is 'replaced'
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'linked_items_replaced_at_consistency'
    ) THEN
        ALTER TABLE linked_items ADD CONSTRAINT linked_items_replaced_at_consistency
            CHECK (
                (status = 'replaced' AND replaced_at IS NOT NULL) OR
                (status != 'replaced')
            );
    END IF;
END $$;

-- =============================================================================
-- Step 4: Create Indexes for Performance
-- =============================================================================

-- Index for finding active items at a specific institution (re-link detection)
CREATE INDEX IF NOT EXISTS idx_linked_items_institution_status
    ON linked_items(user_id, institution_id, status);

-- Index for following the replacement chain
CREATE INDEX IF NOT EXISTS idx_linked_items_replaced_by
    ON linked_items(replaced_by_id)
    WHERE replaced_by_id IS NOT NULL;

-- =============================================================================
-- Step 5: Add Column Comments for Documentation
-- =============================================================================

-- Update status comment to include new values
COMMENT ON COLUMN linked_items.status IS 'Connection status: active (syncing), error, login_required, pending_expiration, revoked, replaced (re-linked), disconnected (manual)';
COMMENT ON COLUMN linked_items.replaced_at IS 'Timestamp when this item was replaced by a re-link. NULL if not replaced.';
COMMENT ON COLUMN linked_items.replaced_by_id IS 'UUID of the linked_item that replaced this one. Forms a chain for multiple re-links.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
