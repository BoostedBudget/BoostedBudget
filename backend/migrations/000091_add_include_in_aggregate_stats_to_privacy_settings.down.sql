-- #972: Revert — drop include_in_aggregate_stats from privacy_settings.

ALTER TABLE privacy_settings DROP COLUMN IF EXISTS include_in_aggregate_stats;
