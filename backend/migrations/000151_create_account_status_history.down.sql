-- Rollback for 000151: drop account_status_history entirely.
--
-- This intentionally discards any history rows written while the table existed (both the 000152
-- backfill rows and anything the 000152 trigger recorded live), and it MIRRORS this repo's other
-- from-scratch-table rollbacks (e.g. 000146's down.sql for invalid_webhook_attempts).
--
-- ⚠ THIS IS NO LONGER A ROLLBACK OF INERT GROUNDWORK. When 000151 landed, nothing read this table
-- and this comment said so. Later commits on the same issue wired FOUR production read paths to
-- it: accountRepository's household as-of and dashboard reads (account_repository.go —
-- accountLiveAsOfPredicate and the household-scope live-as-of query), the unified activity feed's
-- retained-account leg (transaction_repository.go — retainedAccountVisibilityLeg, also used by
-- investment_transaction_repository.go), and portfolio_snapshot_repository.go's Upsert overlap
-- predicate. Dropping the table now makes each of those queries fail against a missing relation:
-- the dashboard, the unified feed and the portfolio snapshot upsert all 500. Rolling back this
-- migration therefore requires rolling back that application code too — it is not a
-- data-discarding no-op.
--
-- 000152 must be rolled back first (its down.sql drops the trigger and function before this
-- statement runs); golang-migrate enforces that ordering automatically.

-- ⚠⚠ AND THE COMMENT ABOVE IS NOT A CONTROL. #1569 task 20 (N11): a rollback is run from a CLI that
-- prints nothing from SQL comments. The guard below refuses while the trail actually holds rows.
--
-- Conditioned on rows rather than unconditional for the same reason 000150's is — see that file's
-- guard: scripts/test-migrations.sh's rollback suite runs `down -all` against a clean_database, and
-- a rollback that can never succeed is a rollback that gets deleted. Dropping an EMPTY table
-- discards nothing.
--
-- Pinned by TestDownMigration000151_RefusesWhileStatusHistoryExists_Integration.

DO $$
DECLARE
    n BIGINT;
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'public' AND table_name = 'account_status_history') THEN
        SELECT COUNT(*) INTO n FROM account_status_history;
        IF n > 0 THEN
            RAISE EXCEPTION
                'refusing to roll back 000151: account_status_history holds % row(s), and this '
                'rollback DROPs the table. That trail is the only record of when each account '
                'changed status, it is what four production read paths join against for their '
                '"as of" answers, and nothing reconstructs it — the 000152 backfill can only infer '
                'the CURRENT status of surviving rows. Export it, or empty it deliberately, before '
                'retrying.', n;
        END IF;
    END IF;
END $$;

DROP TABLE IF EXISTS account_status_history;
