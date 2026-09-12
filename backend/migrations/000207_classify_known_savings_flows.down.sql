-- Retain established provenance. Reclassifying it as unknown would discard facts;
-- the schema rollback separately refuses while classified rows exist.
SELECT 1;
