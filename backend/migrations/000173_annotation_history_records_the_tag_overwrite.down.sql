-- Rollback for 000173 (#1569 gap G12).
--
-- ⚠ Narrowing the CHECK back FAILS if any 'tags' row exists, which is correct and is the same shape
-- 000170's and 000172's rollbacks take: those rows are the only record of a tag set the user lost,
-- and deleting them to make a rollback succeed would destroy exactly the history this migration
-- exists to create. Export them before rolling back if they matter.
--
-- The columns are dropped AFTER the constraint is narrowed, in that order, so a database still
-- holding 'tags' rows fails while their contents are still readable.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review', 'settlement', 'erasure'));

ALTER TABLE transaction_annotation_history
    DROP COLUMN IF EXISTS prior_tags,
    DROP COLUMN IF EXISTS new_tags,
    DROP COLUMN IF EXISTS prior_tags_source,
    DROP COLUMN IF EXISTS new_tags_source;
