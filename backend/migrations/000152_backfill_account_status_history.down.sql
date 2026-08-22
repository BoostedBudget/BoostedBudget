-- Rollback for 000152: drop the two triggers and the function, then delete the rows this
-- migration's backfill inserted.
--
-- Deliberately does NOT attempt to delete only "backfilled" rows and leave "recorded" ones (or
-- vice versa) — 000151's down.sql drops the whole table next in the rollback chain anyway, so a
-- more surgical DELETE here would be extra complexity with no observable benefit. This file only
-- needs to leave `accounts` writable without the trigger before 000151's down.sql runs.

DROP TRIGGER IF EXISTS account_status_history_update_trigger ON accounts;
DROP TRIGGER IF EXISTS account_status_history_insert_trigger ON accounts;
DROP FUNCTION IF EXISTS record_account_status_change();

DELETE FROM account_status_history;
