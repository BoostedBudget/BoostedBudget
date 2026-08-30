-- #1569 round 15 task 9, defect C3 — first half of a two-file index addition. This file clears an
-- INVALID leftover named idx_accounts_user_account_backfill; 000182 builds it.
--
-- WHY TWO FILES, AND WHY THIS ONE EXISTS BEFORE THE INDEX HAS EVER BEEN BUILT. The index is created
-- CONCURRENTLY (see 000182 for why accounts demands that), and CREATE INDEX CONCURRENTLY does not
-- roll away on failure: a deadlock, a cancelled statement or a killed backend leaves a catalogue
-- entry with pg_index.indisvalid = false, maintained on every write and used by no query. The trap
-- is the RETRY: `IF NOT EXISTS` matches on the NAME, not on validity, so re-running the build
-- reports success and leaves the index invalid — measured and written up in 000143, which had to
-- repair exactly that on idx_audit_logs_deletion_residue after the fact. Doing it in the right order
-- here means a re-run after an interrupted build self-heals instead of silently succeeding.
--
-- The conditional DROP needs a DO block and CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction block, and golang-migrate sends a multi-statement file as ONE simple query (which
-- Postgres wraps in an implicit transaction). So the two statements cannot share a file. Do not
-- "tidy" them back together — 000143 records the exact error from trying.
--
-- A plain DROP INDEX is correct rather than DROP INDEX CONCURRENTLY: the target is invalid, so no
-- query can be using it and the brief ACCESS EXCLUSIVE lock has nothing to wait on. A VALID index is
-- left completely untouched. On a database where the index is absent — every database today — this
-- is a clean no-op, which is what makes it idempotent.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_index i ON i.indexrelid = c.oid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'idx_accounts_user_account_backfill'
          AND n.nspname = current_schema()
          AND NOT i.indisvalid
    ) THEN
        EXECUTE 'DROP INDEX idx_accounts_user_account_backfill';
    END IF;
END $$;
