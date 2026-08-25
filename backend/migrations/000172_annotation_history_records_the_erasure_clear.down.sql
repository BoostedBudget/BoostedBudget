-- Rollback for 000172 (#1569 follow-up / #1590).
--
-- ⚠ Narrowing the CHECK back to three values FAILS if any 'erasure' row exists, which is the correct
-- behaviour and the same shape 000170's rollback takes: those rows are the ONLY record of what a
-- surviving user lost when somebody else exercised erasure, and there is no other copy anywhere.
-- Deleting them to make a rollback succeed would destroy exactly the history this migration exists
-- to create. Export them before rolling back if they matter.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review', 'settlement'));
