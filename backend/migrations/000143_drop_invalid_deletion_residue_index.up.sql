-- Round 15, Tier E item 5: drop idx_audit_logs_deletion_residue if a previous CONCURRENTLY build
-- LEFT IT INVALID. This is the first half of a two-file repair; 000144 rebuilds it.
--
-- WHAT GOES WRONG WITHOUT THIS, AND IT IS VERIFIED, NOT REASONED. CREATE INDEX CONCURRENTLY builds in
-- two passes and takes no write-blocking lock, which is why 000138 uses it — audit_logs is the table
-- every write path appends to. The price is that a failure (a deadlock, a cancelled statement, a
-- backend killed mid-build) does NOT roll the index away: it leaves a catalogue entry with
-- pg_index.indisvalid = false. Such an index is maintained on every write and used by NO query.
--
-- The trap is what a re-run then does. Measured on postgres:17.10, against a database whose index had
-- been marked invalid:
--
--   CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_logs_deletion_residue ...
--   NOTICE:  relation "idx_audit_logs_deletion_residue" already exists, skipping
--   CREATE INDEX
--   SELECT indisvalid ... -> f
--
-- IF NOT EXISTS matches on the NAME, not on validity, so the retry reports success and the index is
-- still invalid. 000138's own measurements say what that costs: Index Only Scan, 2 buffers, 0.04 ms
-- becomes Seq Scan, 2470 buffers, 13.4 ms at 200k rows, on a call /admin/health makes on every
-- request, over an append-only table nothing prunes. Neither 000138's up nor its down consults
-- pg_index.indisvalid, so nothing in the system could notice.
--
-- WHY A NEW MIGRATION RATHER THAN A FIX TO 000138. 000138 has been applied to running databases and
-- golang-migrate never re-runs an applied version, so editing it in place would repair no database
-- that has the problem. Same reason 000141 and 000142 exist as re-issues of 000140, and 000136 of
-- 000135.
--
-- WHY TWO FILES AND NOT ONE. This is not a stylistic choice. golang-migrate sends a multi-statement
-- file as one simple query, which Postgres executes inside an implicit transaction block, and
-- CREATE INDEX CONCURRENTLY cannot run in one. Verified by running exactly that combined file:
--
--   pq: CREATE INDEX CONCURRENTLY cannot run inside a transaction block
--
-- So the conditional DROP (which needs a DO block, and is therefore fine inside a transaction) and the
-- CONCURRENTLY rebuild (which must be a file's sole statement, as 000077/000082/000138 all are) cannot
-- share a file. Do not "tidy" them back together.
--
-- SAFETY. A plain DROP INDEX is used rather than DROP INDEX CONCURRENTLY precisely because the target
-- is invalid: no query can be using it, so the brief ACCESS EXCLUSIVE lock has nothing to wait on and
-- nothing to block a read of. DROP INDEX CONCURRENTLY would in any case be illegal here for the same
-- transaction-block reason as above. A VALID index is left completely untouched — this is a repair,
-- not a rebuild, and dropping a healthy index would open a window with no index at all.
--
-- Idempotent: on a database where the index is absent, or present and valid, this is a clean no-op.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_index i ON i.indexrelid = c.oid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'idx_audit_logs_deletion_residue'
          AND n.nspname = current_schema()
          AND NOT i.indisvalid
    ) THEN
        EXECUTE 'DROP INDEX idx_audit_logs_deletion_residue';
    END IF;
END $$;
