-- Issue #1477: a PERSISTED cross-run resume position for the three periodic sweeps whose
-- in-memory keyset cursor starts at nil on every run (scheduled_deletion_check,
-- retention_cleanup_check's deleteExpiredAccounts, item_cleanup_check).
--
-- The defect this closes: each of those sweeps is capped per run. Above the cap, every run
-- re-reads the same oldest rows, so a permanently-failing head of the queue starves everything
-- behind it indefinitely — not "drains slowly". Nothing persisted where a sweep got to.
--
-- ⚠ THIS TABLE DELIBERATELY HAS NO user_id AND NO FOREIGN KEY — the same shape, for the same
-- reason, as plaid_orphaned_items (migration 000134). A sweep cursor MUST outlive the rows it
-- points past: two of the three sweeps exist precisely to hard-delete the users they walk
-- (DeleteUserCascade's `DELETE FROM users`), so any FK into users would either cascade the cursor
-- away mid-sweep or block the erasure the sweep is performing. And a cursor that stored a user id
-- would leave a reference to an ERASED user sitting in the database after a GDPR deletion
-- completed. It therefore stores ONLY the sort-key TIMESTAMPS of the position, never the row
-- identity: see the ts1/ts2/exclusive column comments below for how a timestamp-only watermark is
-- still an exact keyset position (the row-identity component is a synthesized min/max UUID
-- sentinel supplied in Go, never a real id).
--
-- Idempotent per this repo's migration convention (IF NOT EXISTS throughout); paired .down.sql
-- drops the table. Squawk note: this migration only CREATEs a new, initially-empty table and adds
-- COMMENTs. It takes no lock on any existing relation, rewrites nothing, and adds no constraint to
-- an existing table — so none of the rules this repo excludes in .squawk.toml
-- (require-concurrent-index-creation, adding-not-nullable-field, adding-foreign-key-constraint,
-- disallowed-unique-constraint, …) could have applied here even if they were enabled. The NOT NULL
-- columns below carry DEFAULTs and are on a brand-new table, so adding-not-nullable-field is
-- inapplicable rather than merely silenced.

CREATE TABLE IF NOT EXISTS sweep_cursors (
    -- One row per sweep. The PRESENCE of a row is what means "this sweep stopped early and has a
    -- resume position"; a sweep that reached the end of its candidate set DELETEs its row rather
    -- than storing a past-the-end position. That is the whole wrap-around mechanism, and it is why
    -- absence, not a sentinel value, is the "no watermark" encoding: a NULL ts1 is a LEGITIMATE
    -- position (see below), so NULL could not double as "unset".
    sweep_name TEXT PRIMARY KEY,

    -- ts1/ts2 are the leading (non-identity) components of the owning sweep's ORDER BY, in order.
    --
    --   scheduled_deletion  → ts1 = deletion_scheduled_at,  ts2 unused (always NULL)
    --   expired_retention   → ts1 = retention_expires_at,   ts2 unused (always NULL)
    --   item_cleanup        → ts1 = last_error_at,          ts2 = last_synced_at
    --
    -- Nullable because item_cleanup's two sort columns are BOTH nullable and a NULL is a genuine
    -- position: the "zero active accounts" leg of that sweep's predicate matches items that have
    -- never errored and never synced, so a page can end on a row whose entire timestamp key is
    -- (NULL, NULL). The queries coalesce NULL to 'infinity' on both the column and the parameter
    -- side, which is exactly where ORDER BY ... NULLS LAST puts it — see cleanupCandidateSortKey
    -- (repository/linked_item_repository.go).
    ts1 TIMESTAMPTZ,
    ts2 TIMESTAMPTZ,

    -- exclusive distinguishes "resume AT this timestamp key" from "resume STRICTLY AFTER it".
    --
    -- Normally FALSE: resuming inclusively re-visits the handful of rows that tie on the boundary
    -- timestamp, which is harmless (every sweep's per-row action is idempotent or dedup-guarded)
    -- and is the only spelling that cannot SKIP a tied row.
    --
    -- Set TRUE only in the degenerate case where an entire capped run was consumed by rows sharing
    -- ONE timestamp key, so an inclusive resume would replay the identical page forever and
    -- reintroduce the starvation this table exists to fix. It buys guaranteed forward progress at
    -- the cost of skipping the remainder of that one over-cap tie group until the next wrap.
    exclusive BOOLEAN NOT NULL DEFAULT FALSE,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE sweep_cursors IS
    'Issue #1477: persisted cross-run resume position for the capped periodic sweeps. Deliberately '
    'carries NO user_id and NO foreign key (same rationale as plaid_orphaned_items): it must outlive '
    'the rows it walks past, including users erased by the very sweep that advanced it, so it stores '
    'only sort-key timestamps and never a row identity.';

COMMENT ON COLUMN sweep_cursors.sweep_name IS
    'Sweep identifier. Row present = that sweep stopped early and has a resume position; row absent = '
    'start from the beginning. Absence rather than a sentinel, because a NULL ts1 is a legitimate position.';

COMMENT ON COLUMN sweep_cursors.ts1 IS
    'First non-identity ORDER BY component of the owning sweep. NULL is a real position, coalesced to the '
    'timestamptz infinity by the queries to match ORDER BY ... NULLS LAST.';

COMMENT ON COLUMN sweep_cursors.ts2 IS
    'Second non-identity ORDER BY component, used only by the item_cleanup sweep (last_synced_at); NULL for the two user sweeps.';

COMMENT ON COLUMN sweep_cursors.exclusive IS
    'FALSE = resume at this timestamp key (re-visits boundary ties, never skips one). TRUE = resume strictly '
    'after it; set only when a whole capped run was consumed by a single timestamp key, to guarantee forward progress.';
