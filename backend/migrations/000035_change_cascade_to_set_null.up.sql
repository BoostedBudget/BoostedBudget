-- Migration: Change transactions FK constraints from CASCADE to SET NULL
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Safety net - preserve transactions when linked_item or account is deleted
--
-- Current behavior: Deleting a linked_item or account cascades to delete all transactions
-- New behavior: Deleting sets the FK to NULL, preserving transaction data
--
-- This is a SAFETY NET for accidental deletions. The normal re-link flow uses
-- soft-delete (status='replaced'), not hard-delete.

-- =============================================================================
-- Step 1: Make columns nullable (required for SET NULL behavior)
-- =============================================================================

-- Make account_id nullable
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions'
          AND column_name = 'account_id'
          AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE transactions ALTER COLUMN account_id DROP NOT NULL;
    END IF;
END $$;

-- Make linked_item_id nullable
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions'
          AND column_name = 'linked_item_id'
          AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE transactions ALTER COLUMN linked_item_id DROP NOT NULL;
    END IF;
END $$;

-- =============================================================================
-- Step 2: Replace CASCADE constraints with SET NULL
-- =============================================================================

-- Drop existing account_id FK constraint (if exists)
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    -- Find the constraint name (could be auto-generated or explicit)
    SELECT tc.constraint_name INTO constraint_name
    FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu
        ON tc.constraint_name = kcu.constraint_name
        AND tc.table_schema = kcu.table_schema
    WHERE tc.table_name = 'transactions'
      AND tc.constraint_type = 'FOREIGN KEY'
      AND kcu.column_name = 'account_id';

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE transactions DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

-- Add new account_id FK with SET NULL
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
            ON tc.constraint_name = kcu.constraint_name
        WHERE tc.table_name = 'transactions'
          AND tc.constraint_type = 'FOREIGN KEY'
          AND kcu.column_name = 'account_id'
    ) THEN
        ALTER TABLE transactions
        ADD CONSTRAINT transactions_account_id_fkey
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL;
    END IF;
END $$;

-- Drop existing linked_item_id FK constraint (if exists)
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    SELECT tc.constraint_name INTO constraint_name
    FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu
        ON tc.constraint_name = kcu.constraint_name
        AND tc.table_schema = kcu.table_schema
    WHERE tc.table_name = 'transactions'
      AND tc.constraint_type = 'FOREIGN KEY'
      AND kcu.column_name = 'linked_item_id';

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE transactions DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

-- Add new linked_item_id FK with SET NULL
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
            ON tc.constraint_name = kcu.constraint_name
        WHERE tc.table_name = 'transactions'
          AND tc.constraint_type = 'FOREIGN KEY'
          AND kcu.column_name = 'linked_item_id'
    ) THEN
        ALTER TABLE transactions
        ADD CONSTRAINT transactions_linked_item_id_fkey
        FOREIGN KEY (linked_item_id) REFERENCES linked_items(id) ON DELETE SET NULL;
    END IF;
END $$;

-- =============================================================================
-- Step 3: Add comments documenting the change
-- =============================================================================

COMMENT ON COLUMN transactions.account_id IS 'FK to accounts. Nullable for orphan transactions (ON DELETE SET NULL). Use user_account_id for stable queries.';
COMMENT ON COLUMN transactions.linked_item_id IS 'FK to linked_items. Nullable for orphan transactions (ON DELETE SET NULL). Tracks which Plaid Item synced this transaction.';

-- =============================================================================
-- Migration Complete
-- =============================================================================
