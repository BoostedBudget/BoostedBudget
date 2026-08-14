-- Issue #1514: Rollback for the legacy invalid-signature webhook row purge.
-- NOTE: Deleted rows are NOT restored — they were unsigned/invalid-signature audit rows that no
-- longer serve any purpose (the audit signal for such deliveries now lives in
-- invalid_webhook_attempts, migration 000146), and restoring them would re-open the
-- idempotency-poisoning window this migration exists to close.
SELECT 1;
