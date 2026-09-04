-- Rollback for 000183. Drops the column; every cursor row keeps its ts1, and the code that reads
-- ts1_offset is gone with the rollback, so the sweeps revert to the timestamp-only resume they had
-- before -- including its over-cap tie-group residual. No data is lost that outlives the column: an
-- offset is meaningful only next to the code that walks that scan.
ALTER TABLE sweep_cursors DROP COLUMN IF EXISTS ts1_offset;
