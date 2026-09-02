-- Rollback for 000182. Drops only the index; accounts and its rows are untouched. After this the
-- backfill's page query still returns the right rows, by sequential scan and a full sort of the
-- candidate set — see the up migration's measurements for what that costs at a backlog.
--
-- CONCURRENTLY, as the file's sole statement, for the same transaction-block reason as the up
-- migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_user_account_backfill;
