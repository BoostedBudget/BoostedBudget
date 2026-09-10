-- Reverses 000192.
--
-- ⚠ CORRECTED #1625 second fix wave L7. This said "no read reports it to a user" and called the
-- rollback "not data loss". Both were wrong. `version` IS reported: it is selected by GetByID,
-- GetByUserID and queryRules (categorization_rule_repository.go) and serialised on every rule the
-- API returns, so a client holding a rule holds its version. And dropping the column DESTROYS the
-- accumulated token: a later re-up recreates it at the default, so every rule's version resets to
-- 1. A client that read version 7 before the rollback and writes it back after the re-up is
-- refused forever (7 never matches 1), and two clients that both read 1 can no longer be told
-- apart by it.
--
-- What is still true, and why this needs no refusal of the 000180 kind: the token is DERIVED
-- bookkeeping, not user or financial data. Every rule's meaning (name, conditions, target) is
-- untouched, nothing about the user's ledger changes, and the failure mode is a stale editor
-- getting a 409 or a lost update on a concurrent save — recoverable by re-reading the rule.
-- Rolling back also restores the pre-000192 `updated_at`-keyed guard and the false-conflict
-- defect that key implies. A behavioural regression plus a reset counter, not destroyed history.
ALTER TABLE categorization_rules
    DROP COLUMN IF EXISTS version;
