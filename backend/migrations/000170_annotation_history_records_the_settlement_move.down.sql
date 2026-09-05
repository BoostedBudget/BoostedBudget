-- Rollback for 000170 (#1569 Task 6 / #1579, fix round 2).
--
-- ⚠ Narrowing the CHECK back to two values FAILS if any 'settlement' row exists, which is the
-- correct behaviour: those rows are the only record of what a settlement move overwrote, and
-- silently deleting them to make a rollback succeed would destroy exactly the history this issue
-- exists to retain. Export them before rolling back if they matter.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review'));
