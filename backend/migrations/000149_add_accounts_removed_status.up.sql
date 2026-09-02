-- Issue #1569: add a 'removed' status to accounts — this account stopped being connected.
--
-- ⚠ CORRECTED IN PLACE (#1569 T7-FIX). This header, and the COMMENT ON COLUMN at the foot of the
-- file, used to say 'removed' meant "the USER deliberately disconnected this account". That was
-- true on the day this migration was written and became false at migration 000154, which added
-- removed_reason and with it THREE causes: 'user_disconnect', 'provider_absent' (the bank stopped
-- reporting the account) and 'link_removed' (one of this product's own flows removed the Plaid
-- Item — the orphaned-item sweep passes it). MarkRemovedByProvider and MarkRemovedByLinkedItemID
-- both stamp removed_at, so the old wording described a user action that never happened. The
-- corrected COMMENT text is re-issued to already-migrated databases by migration 000175, the same
-- way 000174 re-issued the last correction to this file.
--
-- This is schema groundwork for retaining financial history by default: instead of
-- destroying an account's rows when the user disconnects it, we now have a status value that
-- records *why* the account stopped being active, distinct from the two existing values:
--
--   active   = currently linked and live
--   replaced = superseded by a re-link (existing, 000033)
--   expired  = the 90-day fingerprint TTL released the hold on this row (existing, 000107)
--   removed  = the account stopped being connected (new, this migration). One cause when this
--              file was written (a user disconnect); THREE since 000154 — see the header above.
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
-- Step 3: Pair 'removed' with removed_at, mirroring accounts_replaced_at_consistency.
--
-- One-directional, like the constraint it mirrors: status = 'removed' IMPLIES removed_at IS NOT
-- NULL. A non-'removed' row carrying a stamped removed_at is TOLERATED by this CHECK; keeping that
-- from happening is a Go-level discipline (updateStatusWithRemovalReason NULLs the timestamps it is
-- not stamping; RestoreProviderRemovedAccount NULLs removed_at). See 000155's Step 3 header for why
-- the converse is deliberately left to code. #1569 task 7.9.
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
$COMMENT$Timestamp when this account stopped being connected (status = 'removed'). NOT necessarily a user action: 'removed' has THREE causes and removed_reason (migration 000154) records which one -- 'user_disconnect' (the user asked, via DisconnectAccount or DisconnectInstitution), 'provider_absent' (a confirmed-complete Plaid /accounts/get stopped returning it, i.e. the bank closed it) or 'link_removed' (one of this product's own flows removed the account's Plaid Item). A NULL reason predates 000154 and is treated as a user disconnect. Never NULL while status = 'removed' -- accounts_removed_at_consistency requires it. Left NULL by every Go writer that moves the row to another status, but that direction is a code convention, NOT a constraint: the CHECK is one-directional (see 000155's Step 3 header). Distinct from replaced_at (re-link) and the TTL clock behind 'expired' (linked_items.status_changed_at).$COMMENT$;
