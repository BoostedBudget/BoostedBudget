-- Rollback for 000181. Deliberately a no-op, and that is the only correct rollback: the up
-- migration's entire effect is to remove an index that was already serving no query
-- (pg_index.indisvalid = false). Re-creating an index and marking it invalid on purpose would be
-- manufacturing the fault this file exists to clear. 000182's own down handles the index itself.
SELECT 1;
