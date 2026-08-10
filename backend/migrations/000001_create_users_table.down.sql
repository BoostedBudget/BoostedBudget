-- Rollback: Drop users table and all dependent objects
-- WARNING: This will delete ALL user data
-- Only use in development or with full backup

DROP TABLE IF EXISTS users CASCADE;

-- Drop the trigger helper function created by this migration's up.sql. Ordering-safe: golang-migrate
-- runs .down.sql files in REVERSE numeric order, so every LATER migration that attaches a trigger
-- using this function (000002, 000006, 000007, 000008, 000010, 000011, 000018, 000020, 000022,
-- 000025, 000043, 000044, 000051, 000052, 000096, 000097, 000106-000110 — verified via grep across
-- backend/migrations/*.up.sql) has already had ITS OWN table dropped (CASCADE, taking the trigger
-- with it) by its own .down.sql before this one runs last. CASCADE here is a defensive backstop for
-- a table that survives to this point unexpectedly; in the normal down-all sequence nothing should
-- actually be cascaded away by it.
DROP FUNCTION IF EXISTS update_updated_at_column() CASCADE;
