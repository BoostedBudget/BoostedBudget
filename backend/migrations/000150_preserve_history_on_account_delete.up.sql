-- Issue #1569: stop an account's FK graph from destroying its own history on delete.
--
-- Defence-in-depth only: the real protection against losing history is that accounts stop being
-- hard-deleted in the first place (later tasks in #1569). But as long as an ON DELETE CASCADE
-- exists anywhere below accounts, any future accidental hard-delete would silently wipe out
-- exactly the rows this feature exists to retain. This migration changes the two FKs that would
-- do that:
--
--   1. balance_snapshots.account_id: CASCADE -> SET NULL (and the column becomes nullable).
--      Balance history should survive even if the account row it pointed at is gone.
--
--   2. savings_goals.account_id: CASCADE -> RESTRICT.
--      savings_goals.account_id is NOT NULL ("Required linked account", 000022) and
--      savings_progress_service.go reads goal.AccountID unconditionally — SET NULL here would
--      produce scan failures and invalid domain objects. RESTRICT instead makes a future account
--      purge refuse while a goal still points at it ("delete the goal first"), which is the
--      intended behaviour.
--
-- Every other FK is left alone, in particular every `user_id ... ON DELETE CASCADE`: that FK
-- graph IS the GDPR right-to-erasure mechanism (profile_repository.go, ADR
-- docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md). This migration touches only the two
-- account_id FKs named above.
--
-- Uses this repo's zero-downtime FK convention (canonical example: 000123-000126): drop the old
-- constraint, then ADD CONSTRAINT ... NOT VALID followed by VALIDATE CONSTRAINT, so the
-- validation scan runs under SHARE UPDATE EXCLUSIVE rather than blocking concurrent reads/writes
-- under the ACCESS EXCLUSIVE a plain ADD CONSTRAINT would hold for the same duration.

-- =============================================================================
-- Step 1: balance_snapshots.account_id -- CASCADE -> SET NULL, column becomes nullable
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'balance_snapshots' AND column_name = 'account_id' AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE balance_snapshots ALTER COLUMN account_id DROP NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'balance_snapshots'::regclass
          AND conname = 'fk_balance_snapshots_account_id'
          AND confdeltype != 'n'
    ) THEN
        ALTER TABLE balance_snapshots DROP CONSTRAINT fk_balance_snapshots_account_id;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'balance_snapshots'::regclass
          AND conname = 'fk_balance_snapshots_account_id'
    ) THEN
        ALTER TABLE balance_snapshots
            ADD CONSTRAINT fk_balance_snapshots_account_id
            FOREIGN KEY (account_id) REFERENCES accounts(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'balance_snapshots'::regclass
          AND conname = 'fk_balance_snapshots_account_id'
          AND NOT convalidated
    ) THEN
        ALTER TABLE balance_snapshots VALIDATE CONSTRAINT fk_balance_snapshots_account_id;
    END IF;
END $$;

-- =============================================================================
-- Step 2: savings_goals.account_id -- CASCADE -> RESTRICT (column stays NOT NULL)
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goals'::regclass
          AND conname = 'fk_savings_goals_account_id'
          AND confdeltype != 'r'
    ) THEN
        ALTER TABLE savings_goals DROP CONSTRAINT fk_savings_goals_account_id;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goals'::regclass
          AND conname = 'fk_savings_goals_account_id'
    ) THEN
        ALTER TABLE savings_goals
            ADD CONSTRAINT fk_savings_goals_account_id
            FOREIGN KEY (account_id) REFERENCES accounts(id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goals'::regclass
          AND conname = 'fk_savings_goals_account_id'
          AND NOT convalidated
    ) THEN
        ALTER TABLE savings_goals VALIDATE CONSTRAINT fk_savings_goals_account_id;
    END IF;
END $$;
