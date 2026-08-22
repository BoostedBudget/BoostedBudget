-- Rollback for 000151: drop account_status_history entirely.
--
-- This intentionally discards any history rows written while the table existed (both the 000152
-- backfill rows and anything the 000152 trigger recorded live). That is acceptable for a
-- rollback of new groundwork with no read path wired to it yet (see the "out of scope" list in
-- the #1569 task-3 brief — no read path was ever changed to depend on this table), and mirrors
-- this repo's other from-scratch-table rollbacks (e.g. 000146's down.sql for
-- invalid_webhook_attempts).
--
-- 000152 must be rolled back first (its down.sql drops the trigger and function before this
-- statement runs); golang-migrate enforces that ordering automatically.

DROP TABLE IF EXISTS account_status_history;
