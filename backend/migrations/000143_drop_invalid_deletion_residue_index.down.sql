-- Rollback for 000143. Deliberately a no-op, and that is the only correct rollback.
--
-- The up-migration's entire effect is to remove an index that was ALREADY not serving any query
-- (pg_index.indisvalid = false). There is nothing to restore: re-creating an index and then marking it
-- invalid on purpose would be manufacturing the fault this migration exists to clear. 000144's own
-- down handles the index's existence; this file has no state of its own.
SELECT 1;
