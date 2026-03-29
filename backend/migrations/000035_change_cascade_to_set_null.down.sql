-- Rollback: Restore transactions FK constraints to CASCADE
-- Issue: #171 - Plaid Re-Link Handling
--
-- WARNING: This rollback changes ON DELETE SET NULL back to ON DELETE CASCADE.
-- Any future deletions will cascade-delete transactions!

-- =============================================================================
-- Step 1: Replace SET NULL constraints with CASCADE
-- =============================================================================

-- Drop SET NULL account_id FK constraint
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
      AND kcu.column_name = 'account_id';

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE transactions DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

-- Drop SET NULL linked_item_id FK constraint
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

-- =============================================================================
-- Step 2: Handle orphaned transactions before making columns NOT NULL
-- =============================================================================

-- Delete transactions with NULL account_id (orphans created by SET NULL)
DELETE FROM transactions WHERE account_id IS NULL;

-- Delete transactions with NULL linked_item_id (orphans)
DELETE FROM transactions WHERE linked_item_id IS NULL;

-- =============================================================================
-- Step 3: Make columns NOT NULL again
-- =============================================================================

ALTER TABLE transactions ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE transactions ALTER COLUMN linked_item_id SET NOT NULL;

-- =============================================================================
-- Step 4: Add CASCADE constraints back
-- =============================================================================

ALTER TABLE transactions
ADD CONSTRAINT transactions_account_id_fkey
FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;

ALTER TABLE transactions
ADD CONSTRAINT transactions_linked_item_id_fkey
FOREIGN KEY (linked_item_id) REFERENCES linked_items(id) ON DELETE CASCADE;

-- =============================================================================
-- Step 5: Remove comments
-- =============================================================================

COMMENT ON COLUMN transactions.account_id IS NULL;
COMMENT ON COLUMN transactions.linked_item_id IS NULL;

-- =============================================================================
-- Rollback Complete
-- =============================================================================
