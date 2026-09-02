-- Rollback for 000150: restore both FKs to their pre-migration ON DELETE CASCADE behaviour, and
-- restore balance_snapshots.account_id to NOT NULL.
--
-- Restoring NOT NULL here assumes no row was left with a NULL account_id by SET NULL while this
-- migration was in effect (i.e. no account was actually deleted in between). If that assumption
-- doesn't hold, the ALTER COLUMN SET NOT NULL step below will fail loudly rather than silently
-- corrupting data, which is the correct failure mode for a rollback.
--
-- ⚠⚠ THIS FILE IS A HISTORY-DESTRUCTION PATH. #1569 task 20 (N10). Everything below RE-ARMS the two
-- ON DELETE CASCADE foreign keys that 000150 disarmed. Nothing is destroyed by running it — the
-- destruction happens on the NEXT account delete, when balance_snapshots and savings_goals for that
-- account silently vanish again. savings_goal_contributions go with the goals, and those are money
-- the user logged BY HAND: unrecoverable from Plaid or from any other provider.
--
-- A comment is not a control. A rollback is run by an operator, under pressure, from a CLI that
-- prints nothing from SQL comments. So the guard below REFUSES while any of the rows the re-armed
-- cascades would make destructible still exist.
--
-- ⚠ Why the refusal is conditioned on rows rather than unconditional: scripts/test-migrations.sh's
-- rollback suite runs a full `down -all` → `up` cycle, and an unconditional refusal would make that
-- suite permanently red — which gets guards deleted, not respected. It runs against a
-- clean_database, so this is silent there and loud exactly where it matters. That is also the
-- honest boundary: rolling this back on an empty database destroys nothing.
--
-- Pinned by TestDownMigration000150_RefusesWhileCascadableHistoryExists_Integration, which executes
-- THIS FILE's text against a seeded database and against an empty one.

DO $$
DECLARE
    n_snapshots BIGINT;
    n_goals     BIGINT;
BEGIN
    SELECT COUNT(*) INTO n_snapshots FROM balance_snapshots;
    SELECT COUNT(*) INTO n_goals     FROM savings_goals;
    IF n_snapshots > 0 OR n_goals > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000150: % balance_snapshots row(s) and % savings_goals row(s) exist, '
            'and this rollback re-arms the ON DELETE CASCADE foreign keys that protect them. After it, '
            'deleting one account destroys that account''s balance history and its savings goals — and '
            'with the goals, the savings_goal_contributions the user entered by hand, which no provider '
            'can replace. If you have decided that is acceptable, say so explicitly by emptying those '
            'tables (or by dropping the two constraints yourself) before retrying.', n_snapshots, n_goals;
    END IF;
END $$;

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
