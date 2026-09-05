-- Rollback for 000155: reverse ONLY what the up migration added.
--
-- Dropping expired_at loses the one timestamp this task added; account_status_history and
-- updated_at remain as the only proxies for "when did this expire", exactly the state before this
-- migration. Idempotent: IF EXISTS on both statements.

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_expired_at_consistency;
ALTER TABLE accounts DROP COLUMN IF EXISTS expired_at;
