-- Issue #1477 rollback: drop the persisted sweep-cursor table.
--
-- Dropping it is safe and loses nothing durable: every row is a RESUME POSITION, not a record of
-- work. With the table gone (or empty) each sweep simply starts from the oldest candidate again,
-- which is exactly the pre-#1477 behaviour — the starvation defect returns, but no data is lost
-- and no row is skipped as a result of the rollback itself.
DROP TABLE IF EXISTS sweep_cursors;
