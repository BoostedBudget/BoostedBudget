-- Issue #1569 Task 7: keep the automation trail when a transaction is tombstoned or erased.
-- ⚠ SUPERSEDED CLAIM (2026-08-25, #1569 T8). The statements below asserting that
-- `grep -rn 'rule_execution_history' backend --include='*.go'` matches nothing outside migrations
-- were TRUE WHEN WRITTEN and are now FALSE: #1569 T5 shipped
-- backend/internal/repository/rule_execution_history_repository.go, which both writes and reads
-- this table, plus a service and an endpoint. Nothing about the SCHEMA below changes -- this is a
-- comment-only amendment, made here rather than only in the Go file because a reader who opens
-- this migration has no way to discover a correction that lives somewhere else.
--
--
-- Migration 000020:161 gave rule_execution_history.transaction_id an ON DELETE CASCADE FK. Paired
-- with the hard `DELETE FROM transactions` that Plaid's `removed` array used to trigger, that meant
-- a provider removal silently destroyed the record of every rule that had ever fired on the row --
-- the audit trail for an automation the user configured, deleted by an event the user never caused.
--
-- 000156 replaced that DELETE with a tombstone, so the common case no longer reaches this FK at all.
-- This migration closes the rest of it: on a genuine hard delete the execution record survives with
-- transaction_id NULL, which is what an audit trail is for. The row still names the rule, the
-- action, the result and the time.
--
-- transaction_id must therefore become nullable. unique_rule_transaction UNIQUE (rule_id,
-- transaction_id) is unaffected: Postgres's default NULLS DISTINCT means several orphaned rows for
-- the same rule do not collide.
--
-- GDPR is unaffected, and this is the part worth stating precisely rather than assuming. Erasure is
-- `DELETE FROM users` (profile_repository.go DeleteUserCascade); rule_execution_history has no
-- user_id of its own, and never did -- it is reached through rule_id ->
-- categorization_rules(id) ON DELETE CASCADE (000020:159), whose own user_id -> users(id) is also
-- ON DELETE CASCADE (000020:95). That two-hop path does not involve transaction_id, so flipping
-- this FK cannot leave an erased user's execution rows behind. Pinned by
-- TestTombstone_GDPRErasureStillDestroysIt_Integration, which asserts zero rule_execution_history
-- rows after the erasure (repository/transaction_tombstone_integration_test.go).
--
-- The SET NULL behaviour itself is pinned by TestTombstone_RuleExecutionHistorySurvives_Integration
-- in the same file: the audit row is intact after a tombstone and NULL-but-present after a genuine
-- hard delete.
--
-- No Go code reads or writes this table today (`grep -rn 'rule_execution_history\|RuleExecution'
-- backend --include='*.go'` returns nothing outside migrations). The flip is schema-only, and the
-- integration test is the only thing exercising it.
--
-- Idempotent: the NOT NULL drop is a no-op once applied, and the FK is only replaced when it is not
-- already SET NULL (pg_constraint.confdeltype = 'n').

-- =============================================================================
-- Step 1: transaction_id must accept NULL before SET NULL can ever fire.
-- =============================================================================

-- Dropping NOT NULL is the point of this migration, not an accident: SET NULL cannot fire against
-- a NOT NULL column. squawk's ban-drop-not-null exists to warn that this breaks existing clients;
-- rule_execution_history has none -- `grep -rn "rule_execution_history\|RuleExecution" backend
-- --include="*.go"` matches only migrations -- so the rule is suppressed for THIS STATEMENT ONLY.
-- The directive must sit on its own line immediately above the statement; a trailing
-- `; -- squawk-ignore ...` on the same line is NOT honoured.
-- squawk-ignore ban-drop-not-null
ALTER TABLE rule_execution_history ALTER COLUMN transaction_id DROP NOT NULL;

-- =============================================================================
-- Step 2: Replace CASCADE with SET NULL. Guarded on the existing action so re-running is a no-op
-- and so the statement is never issued against a database that already has the SET NULL form.
-- confdeltype: 'c' = CASCADE, 'n' = SET NULL, 'a' = NO ACTION.
-- =============================================================================

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_transaction_id'
          AND confdeltype <> 'n'
    ) THEN
        ALTER TABLE rule_execution_history
            DROP CONSTRAINT fk_rule_execution_history_transaction_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_transaction_id'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT fk_rule_execution_history_transaction_id
            FOREIGN KEY (transaction_id) REFERENCES transactions(id) ON DELETE SET NULL NOT VALID;
        ALTER TABLE rule_execution_history
            VALIDATE CONSTRAINT fk_rule_execution_history_transaction_id;
    END IF;
END $$;

COMMENT ON COLUMN rule_execution_history.transaction_id IS
$COMMENT$The transaction the rule fired on, or NULL once that transaction has been hard-deleted
(ON DELETE SET NULL since #1569). NULL means "the transaction is gone", never "no transaction" --
every row was written for a real one. Provider removals no longer reach this path at all; they
tombstone the transaction (transactions.deleted_at) and leave this column intact.$COMMENT$;
