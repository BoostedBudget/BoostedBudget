-- #972: Revert creation of savings_impact_snapshots table.
-- Drops the table (and its indexes) entirely.

DROP TABLE IF EXISTS savings_impact_snapshots CASCADE;
