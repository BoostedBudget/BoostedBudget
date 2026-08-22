-- Rollback for 000150: restore both FKs to their pre-migration ON DELETE CASCADE behaviour, and
-- restore balance_snapshots.account_id to NOT NULL.
--
-- Restoring NOT NULL here assumes no row was left with a NULL account_id by SET NULL while this
-- migration was in effect (i.e. no account was actually deleted in between). If that assumption
-- doesn't hold, the ALTER COLUMN SET NOT NULL step below will fail loudly rather than silently
-- corrupting data, which is the correct failure mode for a rollback.

-- =============================================================================
-- Step 1: savings_goals.account_id -- RESTRICT -> CASCADE
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'savings_goals'::regclass
          AND conname = 'fk_savings_goals_account_id'
          AND confdeltype != 'c'
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
            ON DELETE CASCADE
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

-- =============================================================================
-- Step 2: balance_snapshots.account_id -- SET NULL -> CASCADE, column back to NOT NULL
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'balance_snapshots'::regclass
          AND conname = 'fk_balance_snapshots_account_id'
          AND confdeltype != 'c'
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
            ON DELETE CASCADE
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

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'balance_snapshots' AND column_name = 'account_id' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE balance_snapshots ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;
