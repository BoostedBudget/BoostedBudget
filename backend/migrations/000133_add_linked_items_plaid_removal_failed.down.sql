-- Issue #1494: reverse 000133.
--
-- Dropping the columns also drops their COMMENTs, so there is nothing else to undo here.
--
-- DATA LOSS, stated plainly: every recorded "we can never remove this Item" verdict, and the Plaid
-- error code behind it, is destroyed. Nothing else carries the fact for a row that still exists.
--
-- POLICY CONSEQUENCE, which is the opposite direction from 000131's rollback: rolling this back
-- makes DeleteUserCascade's C2 guard block again on a permanently-unremovable item, so it does not
-- re-open a leak — it re-opens the PERMANENT BLOCK issue #1494 exists to close. Rolling back is
-- therefore safe for erasure integrity and unsafe for erasure completion: any user whose only
-- outstanding item is unremovable can never be erased again.
--
-- plaid_orphaned_items (000134) is NOT touched here, deliberately: rows already written to it
-- record live vendor resources we remain obliged to act on, and that obligation does not end
-- because a column was dropped.
--
-- Idempotent: DROP COLUMN IF EXISTS.
ALTER TABLE linked_items DROP COLUMN IF EXISTS plaid_removal_error_code;
ALTER TABLE linked_items DROP COLUMN IF EXISTS plaid_removal_failed_at;
