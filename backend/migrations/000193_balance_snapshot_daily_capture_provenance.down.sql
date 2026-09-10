-- Rollback for 000193. Restores migration 000016's wording of the source column comment.
--
-- The conditional DROP in the up migration has no inverse and needs none: it destroys only an
-- INVALID index left behind by an interrupted CONCURRENTLY build, which is state no rollback should
-- want to recreate. No rows are touched by either direction of this migration.
COMMENT ON COLUMN balance_snapshots.source IS
'Source of the snapshot: plaid_webhook, migration, or manual';
