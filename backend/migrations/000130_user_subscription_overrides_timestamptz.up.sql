-- Issue #1461/#1464 follow-up (db-migration-guide/SKILL.md, "Migrations do not receive the
-- application pool's UTC time zone pin"; migration_tz_hazard_scan_test.go's tzHazardAllowlist
-- entry for 000004_create_subscription_overrides_table.up.sql).
--
-- user_subscription_overrides.expires_at/created_at/updated_at (000004) were created as naked
-- TIMESTAMP (no time zone) columns — the only "CURRENT_TIMESTAMP_untz_column" occurrence the
-- static scanner (migration_tz_hazard_scan_test.go) finds across all 129 prior migrations. This
-- migration converts all three to TIMESTAMPTZ so comparisons and writes against them stop
-- depending on the connecting session's time zone.
--
-- Consequences of the naked columns that this closes (postgres_subscription_override_repository.go):
--   :44  GetActiveOverrides — `WHERE ... (expires_at IS NULL OR expires_at > $2)`, where $2 is a
--        caller-supplied `asOf` parameter (not NOW()), compares a naked TIMESTAMP against the bound
--        TIMESTAMPTZ parameter. Postgres implicitly interprets the naked value in the SESSION time
--        zone to perform that comparison, so the same stored row can read as expired or
--        still-active depending only on which zone the connection happens to be in — reproduced
--        deterministically in migration_000130_subscription_overrides_tz_integration_test.go.
--   :124 UpdateOverride — `updated_at = NOW()` assigns an absolute instant into the naked column;
--        the wall-clock digits actually stored reflect the writing session's zone, not UTC.
--   :30  CreateOverride — inserts a Go time.Time into the naked column via pgx; pgx's
--        TimestampCodec discards the time.Time's Location and keeps only its wall-clock fields,
--        so this is not rescued on the client side either.
--
-- NOT a live production bug today: database.NewPool/NewPoolFromURL pins every APPLICATION
-- connection's session TimeZone to "UTC" (Issue #1461), so the coercions above currently resolve
-- to the same thing an already-correct TIMESTAMPTZ column would. But that correctness rests
-- entirely on the pool pin, with no SQL-level guarantee behind it — exactly the dependency #1461
-- existed to remove — and any reader/writer that does NOT go through that pool (psql, the
-- standalone `migrate` CLI, one-off admin scripts) is not covered by it and can disagree. This
-- migration removes that dependency at the schema level instead of relying on every future caller
-- remembering to connect through the pinned pool.
--
-- This is a POLICY change going forward, not a data-recovery step: every historical wall-clock
-- value already stored in these three columns is henceforth READ AS UTC (see the USING clauses
-- below). It cannot, and does not attempt to, recover which session zone actually produced any
-- given historical row — there is no provenance marker recording that. Separately, and only as
-- context for why this is a low-risk conversion in practice: the sole code path that has ever
-- written expires_at (SubscriptionService.ApplyOverride, called from
-- AdminHandler.CreateSubscriptionOverride) never sets it, and the admin request body
-- (AdminCreateOverrideInput) has no expires_at field for this table at all — so as of this
-- migration, no code path has ever populated a non-null expires_at, and created_at/updated_at are
-- always written under the UTC-pinned application pool. That observation does not change what
-- this migration does (every row, present or future, is read as UTC going forward); it only means
-- the reinterpretation is not expected to change any currently-stored row's effective meaning.
--
-- Locking: ALTER COLUMN ... TYPE rewrites the table and any index depending on the altered
-- column, taking ACCESS EXCLUSIVE for the duration (Postgres automatically rebuilds dependent
-- indexes as part of the ALTER — no separate DROP/CREATE INDEX step is needed here, unlike
-- 000017's balance_snapshots migration, which also renamed the column and introduced new
-- functional indexes). idx_user_subscription_overrides_expires_at (000004:21-23) is the only
-- index, view, generated column, constraint, or trigger anywhere in the migration history that
-- references these columns (verified: no migration after 000004 mentions
-- user_subscription_overrides at all). Squawk's `changing-column-type` rule would normally flag
-- an ALTER COLUMN TYPE, but that rule is excluded repo-wide (.squawk.toml, "Intentional schema
-- evolutions") — a blanket policy decision, not a review of this specific migration, so the safety
-- case is made here instead of relying on a clean lint run: user_subscription_overrides is a
-- low-traffic, admin-only table (enterprise/promo overrides; not a user-facing hot path), so
-- taking an ACCESS EXCLUSIVE lock on it for the brief duration of this rewrite is acceptable.
--
-- SET LOCAL TIME ZONE is deliberately NOT used here. `make migrate-up` pipes this file into
-- `psql -f -` in autocommit — confirmed empirically (see .squawk.toml's assume_in_transaction
-- comment: two `SELECT txid_current()` statements sent via one `psql -f -` invocation returned two
-- different txids, and an INSERT that ran before a later failing statement in the same file was
-- NOT rolled back) — so a `SET LOCAL` here would apply to at most the single autocommitted
-- statement it appears in and be silently inert for every other statement in this file. Every
-- USING clause below pins UTC explicitly via `AT TIME ZONE 'UTC'` instead, which carries its own
-- meaning independent of session state.
--
-- Idempotent: all three columns are converted together inside one DO block, which `psql -f -`
-- sends and executes as a single autocommitted statement — so this migration cannot partially
-- apply (some columns converted, others not) the way a sequence of separate top-level ALTER
-- statements could under `psql -f -`'s per-statement autocommit. That atomicity means only two
-- states are reachable by re-running this file: all three columns still the pre-migration naked
-- TIMESTAMP (convert), or all three already TIMESTAMPTZ (no-op). The guard below checks all
-- three columns (not just created_at) and treats any OTHER combination — which this migration's
-- own atomicity argument says should be unreachable — as an error rather than silently no-oping
-- or silently re-running `AT TIME ZONE 'UTC'` against an already-zoned column: if a mixed state
-- is ever observed anyway (manual intervention, a restored dump, a future edit that splits this
-- block), that is a real anomaly and should fail loudly.
DO $$
DECLARE
    created_at_type TEXT;
    updated_at_type TEXT;
    expires_at_type TEXT;
BEGIN
    SELECT data_type INTO created_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'created_at'
          AND table_schema = current_schema();
    SELECT data_type INTO updated_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'updated_at'
          AND table_schema = current_schema();
    SELECT data_type INTO expires_at_type FROM information_schema.columns
        WHERE table_name = 'user_subscription_overrides' AND column_name = 'expires_at'
          AND table_schema = current_schema();

    IF created_at_type = 'timestamp with time zone'
       AND updated_at_type = 'timestamp with time zone'
       AND expires_at_type = 'timestamp with time zone' THEN
        -- Already converted (e.g. a prior application of this file) — no-op.
        NULL;
    ELSIF created_at_type = 'timestamp without time zone'
       AND updated_at_type = 'timestamp without time zone'
       AND expires_at_type = 'timestamp without time zone' THEN
        -- Drop and recreate the created_at/updated_at DEFAULTs explicitly around the type change,
        -- rather than relying on an implicit coercion of the existing DEFAULT expression during
        -- ALTER COLUMN TYPE. (DEFAULT CURRENT_TIMESTAMP into a TIMESTAMPTZ column is itself safe —
        -- CURRENT_TIMESTAMP is an absolute instant, unlike the naked-TIMESTAMP DEFAULT it
        -- replaces — but this migration does not depend on ALTER COLUMN TYPE's own default-
        -- rewriting behavior to get there.)
        ALTER TABLE user_subscription_overrides ALTER COLUMN created_at DROP DEFAULT;
        ALTER TABLE user_subscription_overrides ALTER COLUMN updated_at DROP DEFAULT;

        ALTER TABLE user_subscription_overrides
            ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC';
        ALTER TABLE user_subscription_overrides
            ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
        ALTER TABLE user_subscription_overrides
            ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';

        ALTER TABLE user_subscription_overrides ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
        ALTER TABLE user_subscription_overrides ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
    ELSE
        RAISE EXCEPTION 'Migration 000130: user_subscription_overrides has a mixed timestamp/timestamptz state that should be unreachable (created_at=%, updated_at=%, expires_at=%) — refusing to silently no-op or silently re-widen an already-converted column.',
            created_at_type, updated_at_type, expires_at_type;
    END IF;
END $$;

COMMENT ON COLUMN user_subscription_overrides.expires_at IS
    'When this override expires (UTC), NULL for permanent overrides. Converted from a zone-naked '
    'TIMESTAMP to TIMESTAMPTZ by migration 000130 (Issue #1461/#1464 follow-up); every value '
    'stored before that migration is being READ AS UTC going forward, not recovered from any '
    'recorded provenance of which zone actually wrote it.';
COMMENT ON COLUMN user_subscription_overrides.created_at IS
    'Row creation time (UTC). Converted from a zone-naked TIMESTAMP to TIMESTAMPTZ by migration '
    '000130 (Issue #1461/#1464 follow-up).';
COMMENT ON COLUMN user_subscription_overrides.updated_at IS
    'Row last-updated time (UTC). Converted from a zone-naked TIMESTAMP to TIMESTAMPTZ by '
    'migration 000130 (Issue #1461/#1464 follow-up).';
