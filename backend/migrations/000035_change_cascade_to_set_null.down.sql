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
--
-- ⚠⚠ THIS IS A HISTORY-DESTRUCTION PATH (#1569 round 13 task 1). The two DELETEs below existed
-- unconditionally from the day this file was written, years before #1569 made "retain financial
-- history by default" a rule. account_id/linked_item_id going NULL here is not migration noise —
-- it is EXACTLY the retained-history signal this whole project exists to preserve: a transaction
-- whose account or linked_item was disconnected keeps its own row, FK set to NULL, instead of being
-- cascade-deleted. Rolling this migration back would delete every one of those retained rows to
-- make room for restoring the old CASCADE behaviour.
--
-- A comment is not a control: a rollback is run by an operator under pressure, from a CLI, and
-- golang-migrate prints nothing from a SQL comment. So this refuses while either kind of retained
-- row still exists.
--
-- ⚠ Conditioned on rows, not unconditional — same reason as 000150's and 000151's guards:
-- scripts/test-migrations.sh's rollback suite runs a full `down -all` → `up` cycle against a
-- database with nothing retained yet, and an unconditional refusal would make that suite
-- permanently red, which gets guards deleted rather than respected. Deleting from an ALREADY-EMPTY
-- set of orphans destroys nothing.
--
-- Pinned by TestDownMigration000035_RefusesWhileOrphanedTransactionsExist_Integration.
DO $$
DECLARE
    n_no_account BIGINT;
    n_no_item    BIGINT;
BEGIN
    SELECT COUNT(*) INTO n_no_account FROM transactions WHERE account_id IS NULL;
    SELECT COUNT(*) INTO n_no_item    FROM transactions WHERE linked_item_id IS NULL;
    IF n_no_account > 0 OR n_no_item > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000035: % transaction(s) with a NULL account_id and % with a NULL '
            'linked_item_id exist. Those NULLs are retained transaction history from a disconnected '
            'account or linked_item (ON DELETE SET NULL, not a cascade delete) — the exact thing '
            '#1569 requires this codebase to keep by default. This rollback''s next two steps '
            'unconditionally DELETE every one of those rows to make account_id/linked_item_id '
            'NOT NULL again. If you have decided that history really should be discarded, export it '
            'or delete those rows deliberately before retrying.', n_no_account, n_no_item;
    END IF;
END $$;

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
