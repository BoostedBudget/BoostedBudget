-- Issue #1569: add a 'removed' status to accounts — the USER deliberately disconnected this
-- account. This is schema groundwork for retaining financial history by default: instead of
-- destroying an account's rows when the user disconnects it, we now have a status value that
-- records *why* the account stopped being active, distinct from the two existing values:
--
--   active   = currently linked and live
--   replaced = superseded by a re-link (existing, 000033)
--   expired  = the 90-day fingerprint TTL released the hold on this row (existing, 000107)
--   removed  = the USER deliberately disconnected this account (new, this migration)
--
-- Conflating 'removed' with 'replaced' or 'expired' would lose the reason in exactly the table
-- we are building to record reasons: a re-link, a TTL expiry, and a deliberate disconnect are
-- three different facts about an account's lifecycle, and later tasks in #1569 (account status
-- history) depend on being able to tell them apart.
--
-- ⚠ Every existence guard below is scoped to THIS table (`conrelid = 'accounts'::regclass`,
-- `table_schema = current_schema()`), and that is load-bearing rather than tidy. pg_constraint is
-- database-wide while constraint names are unique only per table: with a bare
-- `conname = 'accounts_valid_status'` test, a same-named constraint on any other table answers
-- the guard, so step 2's table-scoped DROP succeeds and its re-add is then skipped as
-- "already present" — leaving accounts with NO status CHECK and the migration reporting success.
-- Pinned by TestMigration000149_StatusCheckGuardsAreTableScoped, which plants exactly that decoy
-- and runs this file's own bytes. Issue #1569 residual E11.
--
-- Follows the CHECK-widening idiom from 000062 (drop-if-exists, re-add, inside an idempotent
-- DO $$ ... END $$ block) and the status+timestamp pairing precedent from 000107's
-- chk_household_members_status_removed_at, mirroring the existing sibling constraint
-- accounts_replaced_at_consistency (000033).

-- =============================================================================
-- Step 1: Add removed_at column (Idempotent)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'accounts' AND column_name = 'removed_at'
    ) THEN
        ALTER TABLE accounts ADD COLUMN removed_at TIMESTAMPTZ;
    END IF;
END $$;

-- =============================================================================
-- Step 2: Widen accounts_valid_status to include 'removed'
-- =============================================================================

DO $$
BEGIN
    -- Drop the old constraint (covers 'active', 'replaced', 'expired' only)
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts DROP CONSTRAINT accounts_valid_status;
    END IF;

    -- Add expanded constraint
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_valid_status'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_valid_status
            CHECK (status IN ('active', 'replaced', 'expired', 'removed'));
    END IF;
END $$;

-- =============================================================================
-- Step 3: Pair 'removed' with removed_at, mirroring accounts_replaced_at_consistency
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_removed_at_consistency'
    ) THEN
        ALTER TABLE accounts ADD CONSTRAINT accounts_removed_at_consistency
            CHECK (
                (status = 'removed' AND removed_at IS NOT NULL) OR (status <> 'removed')
            );
    END IF;
END $$;

COMMENT ON COLUMN accounts.removed_at IS
$COMMENT$Timestamp when the user deliberately disconnected this account (status = 'removed'). NULL for every other status. Distinct from replaced_at (re-link) and the TTL clock behind 'expired' (linked_items.status_changed_at).$COMMENT$;
