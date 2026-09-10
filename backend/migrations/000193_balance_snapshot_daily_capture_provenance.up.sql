-- #1626 Phase 3, Defect 3 (Task G) — first half of a two-file index addition, plus the column
-- comment that documents the new provenance value. 000194 builds the index.
--
-- WHAT THIS FILE DOES, AND WHY THE TWO THINGS SHARE IT: both statements are plain, idempotent and
-- transaction-safe, so they can live together; the index build in 000194 can share a file with
-- NOTHING (see below). Keeping the count at two files rather than three keeps the interrupted-run
-- repair a single `force 192`.
--
-- 1. THE COLUMN COMMENT. Migration 000016 documented the source value set inline
--    ("Values: 'plaid_webhook', 'migration', 'manual'") and again as a COMMENT ON COLUMN. The
--    periodic gap-filler adds a fourth, domain.SnapshotSourceDailyCapture ('daily_capture'), so
--    the comment on the live database is now wrong. Reissuing it is the established pattern here
--    (000186, 000188 do exactly this and nothing else). The 000016 FILE is history and is not
--    edited; the database's comment is what a DBA actually reads.
--
-- 2. THE CONDITIONAL DROP OF AN INVALID LEFTOVER. 000194 creates its index CONCURRENTLY, and
--    CREATE INDEX CONCURRENTLY does not roll away on failure: a deadlock, a cancelled statement or
--    a killed backend leaves a catalogue entry with pg_index.indisvalid = false, maintained on
--    every write and used by no query. The trap is the RETRY — `IF NOT EXISTS` matches on the NAME,
--    not on validity, so re-running the build reports success and leaves the index invalid. This
--    repo has already had to repair exactly that after the fact (000143, on
--    idx_audit_logs_deletion_residue) and has since adopted the drop-first ordering (000181/000182,
--    on idx_accounts_user_account_backfill). This file is that ordering, and it is a clean no-op on
--    every database where the index is absent or valid — which is every database today.
--
--    ⚠ THE RE-RUN IS NOT AUTOMATIC. An interrupted 000194 also leaves golang-migrate's version
--    DIRTY at 194, and backend/scripts/docker-entrypoint.sh only retries `migrate up`, which
--    refuses to run while dirty. The repair is an operator action, and the version it forces to is
--    determined by a principle, not by a constant: force to the version IMMEDIATELY BEFORE THIS
--    PAIR, so that `migrate up` replays THIS file's conditional drop first and only then lets
--    000194 rebuild. This pair is 000193/000194, so the version before it is 192 — NOT 193.
--    Forcing to 193 would re-run only 000194, whose IF NOT EXISTS matches on the NAME and not on
--    validity, so the build would report success and leave the invalid index in place:
--
--        migrate -database "$DATABASE_URL" -path migrations force 192
--        migrate -database "$DATABASE_URL" -path migrations up
--
--    A plain DROP INDEX is correct rather than DROP INDEX CONCURRENTLY: the target is invalid, so
--    no query can be using it and the brief ACCESS EXCLUSIVE lock has nothing to wait on. A VALID
--    index is left completely untouched, which is what makes the guard safe to re-run.

COMMENT ON COLUMN balance_snapshots.source IS
'Source of the snapshot: plaid_webhook, migration, manual, or daily_capture. daily_capture rows are written by the periodic balance-snapshot gap-filler (#1626 Phase 3) and are unique per (account_id, UTC day) — see uq_balance_snapshots_daily_capture. The other three sources are deliberately NOT constrained that way: migration 000017 dropped per-day uniqueness because a webhook sync may legitimately record several rows for one account in one day.';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_index i ON i.indexrelid = c.oid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'uq_balance_snapshots_daily_capture'
          AND n.nspname = current_schema()
          AND NOT i.indisvalid
    ) THEN
        EXECUTE 'DROP INDEX uq_balance_snapshots_daily_capture';
    END IF;
END $$;
