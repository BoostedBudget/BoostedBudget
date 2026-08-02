---
name: db-migration-guide
description: "Database migration guide for BoostedBudget: golang-migrate + Squawk migrations, idempotency patterns, rollback support, and testing."
---

# Database Migration Guide

## Overview

BoostedBudget uses **golang-migrate** for migration execution and **Squawk** for PostgreSQL linting. All migrations must be:

1. **Idempotent** - Safe to run multiple times
2. **Reversible** - Have both .up.sql and .down.sql
3. **Tested** - Pass the 5-suite migration test

**Issue #631:** Migrated from Atlas to golang-migrate + Squawk to avoid vendor lock-in.

## File Structure

```
backend/migrations/
├── 000001_create_users_table.up.sql
├── 000001_create_users_table.down.sql
├── 000002_create_accounts_table.up.sql
├── 000002_create_accounts_table.down.sql
└── ...
```

## Creating a New Migration

### Step 1: Determine Next Number

```bash
# Find the highest migration number
ls backend/migrations/*.up.sql | sort -V | tail -1
# Example: 000015_add_dashboard_layouts.up.sql → next is 000016
```

### Step 2: Create Files

```bash
# Create both up and down files
touch backend/migrations/000016_description.up.sql
touch backend/migrations/000016_description.down.sql
```

## Idempotency Patterns

### Creating Tables

```sql
-- 000016_add_feature_flags.up.sql

CREATE TABLE IF NOT EXISTS feature_flags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    flag_name VARCHAR(100) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_feature_flag UNIQUE (user_id, flag_name)
);

-- Create index
CREATE INDEX IF NOT EXISTS idx_feature_flags_user_id ON feature_flags(user_id);
CREATE INDEX IF NOT EXISTS idx_feature_flags_name ON feature_flags(flag_name);
```

### Adding Columns

```sql
-- Use DO block with IF NOT EXISTS check
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'users'
        AND column_name = 'preferences'
    ) THEN
        ALTER TABLE users ADD COLUMN preferences JSONB DEFAULT '{}';
    END IF;
END $$;
```

### Adding Constraints

```sql
-- Check if constraint exists before adding
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_amount_positive'
    ) THEN
        ALTER TABLE transactions
        ADD CONSTRAINT chk_amount_positive
        CHECK (amount IS NOT NULL);
    END IF;
END $$;
```

### Adding Foreign Keys

```sql
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_transactions_account'
    ) THEN
        ALTER TABLE transactions
        ADD CONSTRAINT fk_transactions_account
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE;
    END IF;
END $$;
```

### Adding Indexes

```sql
-- Index creation is naturally idempotent with IF NOT EXISTS
CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(date);
CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_external ON transactions(external_transaction_id);
```

### Adding Enum Values

```sql
-- Enums require special handling
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_enum
        WHERE enumlabel = 'new_value'
        AND enumtypid = 'status_enum'::regtype
    ) THEN
        ALTER TYPE status_enum ADD VALUE 'new_value';
    END IF;
END $$;
```

## Grace-Window Timestamps and Large Backfills (Issue #1464)

An adversarial audit of migrations `000119`-`000122` (Issue #1413 / PR #1443) found two hazard
classes that every existing gate — Squawk and the 5-suite migration test — passed cleanly. Both
are recorded here as conventions so they don't recur, not because those migrations were wrong to
merge (they are already applied and immutable; see the down-migration note below).

### Use a statement-time clock for grace-window/deadline anchors, never `NOW()`

**A migration that writes a grace-window or deadline anchor column must compute that value with a
statement-time clock (`clock_timestamp()`), not `NOW()`/`CURRENT_TIMESTAMP`.**

PostgreSQL evaluates `NOW()` and `CURRENT_TIMESTAMP` once, at **transaction start** — not at the
moment the statement actually runs. A migration transaction that runs long, or blocks waiting on a
lock, writes a marker that is **backdated by however long the transaction took to reach that
statement**, while application code compares the marker against the live, current clock. If the
anchor gates an irreversible action, that gap silently eats into (or eliminates) the grace period
the anchor exists to provide.

This is not hypothetical for this codebase: `users.plaid_cleanup_pending_at` anchors a 72-hour
grace window before a lapsed user's Plaid bank connections are disconnected — an irreversible
external API call. Real instances that use `NOW()` for this column:

- `000120_fix_plaid_cleanup_pending_at_backfill.up.sql:30` — `SET plaid_cleanup_pending_at = NOW()`, with no floor at all.
- `000119_add_plaid_cleanup_pending_at.up.sql:47-48` and `000121_backfill_plaid_cleanup_pending_at_excluded_overrides.up.sql:56-57` — both compute their 48-hour safety floor as `NOW() - INTERVAL '48 hours'`.

PR #1443 spent rounds 19-23 migrating every **application** writer of this same column
(`StampMissingPlaidCleanupMarkers`, `UpdateSubscriptionFromPaddle`, `removeUserTierOverrideUpdate`)
off `NOW()` onto `clock_timestamp()`, for exactly this reason — see
`ExpireTrialIfNoOverride` in `backend/internal/repository/profile_repository.go` for the corrected
pattern. The migrations that wrote the same column were never held to the same standard. Apply the
lesson to migrations too: anywhere a migration stamps a grace-window or deadline column, use
`clock_timestamp()`.

### Never combine DDL with a large backfill in one transaction

**Split a schema change and its data backfill into two separate migrations**: one migration that
does only the `ALTER` (fast, holds its lock briefly), then a follow-up migration that does the
backfill.

Each migration file runs as a single transaction by default. If an `ALTER TABLE` and a full-table
`UPDATE` share that transaction, the `ALTER`'s lock — `ACCESS EXCLUSIVE` for `ADD COLUMN` — is held
for the **entire transaction**, not just for the instant the `ALTER` statement executes. Every
read and write against that table blocks until the backfill finishes and the transaction commits.

Real instance: `000119_add_plaid_cleanup_pending_at.up.sql` does `ALTER TABLE users ADD COLUMN IF
NOT EXISTS plaid_cleanup_pending_at TIMESTAMPTZ;` at line 27, then a full-table `UPDATE users ...`
backfill at lines 45-52, in the same transaction. At production row counts, that holds
`ACCESS EXCLUSIVE` on `users` — one of the hottest tables in the schema — for as long as the
backfill takes to run, blocking subscription, admin, deletion, and webhook writes for that entire
window.

### Batch large backfills

Where a backfill must touch a large or unbounded number of rows, **batch it** — a bounded loop over
a key range (e.g. `WHERE id BETWEEN $1 AND $2`, looping in application code or a `DO` block with a
row-count-limited `UPDATE ... LIMIT`) rather than one unbounded full-table `UPDATE`. A batched
backfill holds its row locks for a fraction of a second per batch instead of monopolizing them for
the whole run.

### Adding `NOT NULL` to an existing column: never a bare `SET NOT NULL`

`ALTER TABLE ... ALTER COLUMN x SET NOT NULL` performs a **mandatory full-table validation scan while
holding `ACCESS EXCLUSIVE`**, blocking every read and write to the table until it finishes. Backfilling
the NULLs in an earlier migration does **not** avoid this: the scan still reads every row regardless of
whether it finds a violation. (Issue #1444's first draft made exactly that mistake, and argued the scan
was cheap "because the backfill already ran" — a Codex adversarial review caught it.)

Use the three-step transition instead, **one migration per step**:

```sql
-- Step 1 (migration N):   instant. ACCESS EXCLUSIVE for a catalog write only, NO scan.
ALTER TABLE t ADD CONSTRAINT t_x_not_null CHECK (x IS NOT NULL) NOT VALID;

-- Step 2 (migration N+1): performs the scan under SHARE UPDATE EXCLUSIVE, which does NOT block
--                         concurrent SELECT/INSERT/UPDATE/DELETE. This is the point of the split.
ALTER TABLE t VALIDATE CONSTRAINT t_x_not_null;

-- Step 3 (migration N+2): PostgreSQL 12+ proves non-nullness from the VALIDATED check and SKIPS the
--                         scan; then drop the now-redundant constraint.
ALTER TABLE t ALTER COLUMN x SET NOT NULL;
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_x_not_null;
```

**The steps must be separate migrations, not separate statements in one file.** golang-migrate wraps
each migration in a single transaction, so combining them holds step 1's `ACCESS EXCLUSIVE` right
through step 2's scan — reproducing the exact hazard. This is the same reason DDL and backfills must be
split (above).

Two properties worth knowing:

- **A `NOT VALID` CHECK still rejects new rows.** `NOT VALID` exempts only rows that already existed
  when the constraint was added, so from step 1 onward no fresh NULL can be introduced even though the
  column is still formally nullable. There is no window of weakened enforcement.
- **Step 2 is where a bad deploy fails, and it fails safely.** If a NULL survived the backfill, `VALIDATE`
  errors — leaving the column nullable and the constraint un-validated, a coherent state the application
  still runs against. Worth a dedicated migration test asserting this fail-closed behaviour, paired with
  a clean-data test that must succeed, so the pair distinguishes on data rather than passing vacuously.

Reference implementation: `000124`/`000125`/`000126` (`linked_items.status`), with
`backend/internal/database/migration_000123_000126_integration_test.go` covering all four steps.

Lock modes verified empirically rather than assumed: while `VALIDATE CONSTRAINT` was held open in a
transaction, `pg_locks` reported `ShareUpdateExclusiveLock` and a concurrent `SELECT` completed; the
identical `SELECT` against a deliberately held `ACCESS EXCLUSIVE` lock failed with `canceling statement
due to statement timeout`. If you change this pattern, re-verify the same way — Squawk cannot tell you
(see below).

### Escape hatch: `ADD COLUMN IF NOT EXISTS` lets an operator pre-empt the lock

Because `000119`'s `ALTER` is written as `ADD COLUMN IF NOT EXISTS`, an operator can run that exact
`ALTER` ahead of time, in its own short transaction, during a low-traffic window. When the real
migration runs later, its `ALTER ... IF NOT EXISTS` becomes a no-op (the column already exists), so
no `ACCESS EXCLUSIVE` lock is acquired at all when the backfill statement runs — only the backfill's
own (still worth batching) row locks apply. This is a useful general mitigation for a DDL-plus-backfill
migration that, for whatever reason, can't be split into two files before it ships: write the `ALTER`
as `IF NOT EXISTS` even when a same-migration backfill follows it, so the escape hatch exists even if
it isn't used.

### Why Squawk and the CI migration-test suites do not catch either of these

Neither hazard above trips an existing gate — this is *why* they need to be a documented convention
rather than something to rely on tooling to flag:

- **Squawk lints statements, not transactions.** `squawk --pg-version=17` inspects each SQL
  statement in isolation. An `ALTER TABLE ADD COLUMN` statement is fine on its own; a full-table
  `UPDATE` is fine on its own. The hazard is entirely that the two statements share one transaction
  — a property Squawk has no visibility into. It reported 0 issues across all nine of #1443's
  migrations.
- **`scripts/test-migrations.sh`'s five suites (idempotency, perf, data, schema, rollback) run
  against a near-empty database.** A full-table backfill over a handful of test rows is
  instantaneous, so the "perf" suite sees no slow migration and the lock-duration hazard never
  materializes in CI. The job reported `success` for the same reason it would report success on a
  migration that scales linearly to hours at production row counts.

**A green Squawk run and a green Migration Tests job are not evidence that a migration is safe to
apply against a production-sized table.** Review DDL-plus-backfill migrations and grace-window
timestamp writers by eye against the two conventions above; don't rely on the gates having caught
it.

### Migrations do not receive the application pool's UTC time zone pin (Issue #1461/#1464 follow-up)

**Any calendar arithmetic in a migration must pin its time zone explicitly with `AT TIME ZONE
'UTC'` — the session-default time zone a migration runs under is not UTC and is not guaranteed.**

`backend/internal/database/pgx.go` sets `poolConfig.ConnConfig.RuntimeParams["TimeZone"] = "UTC"`
on every connection the **application** opens (Issue #1461), because Postgres performs
calendar-interval arithmetic — `make_interval(months => N)`, `INTERVAL 'N days'`, `CURRENT_DATE` —
in the **session** time zone, not UTC, and the unpinned session default (confirmed locally to be
`America/New_York`) silently shifts the computed instant depending on deployment environment and
DST.

**No migration path goes through that pool.** Every way this repo applies migrations shells out to
either `psql` or the standalone `migrate` CLI against `$DATABASE_URL`, none of which touch
`pgx.go`'s `RuntimeParams`:

- `Makefile`'s `migrate-up`/`migrate-down` pipe each `.sql` file into `docker-compose exec ...
  psql`.
- `.github/workflows/test.yml`'s `backend-integration-test` job runs `migrate -database
  "$DATABASE_URL" -path backend/migrations up`.
- `backend/scripts/docker-entrypoint.sh` and `scripts/test-migrations.sh` both run the same
  `migrate -database ... up` CLI invocation.

So a migration doing calendar arithmetic on a `timestamptz` runs under whatever time zone the
connecting Postgres server/session defaults to — silently reintroducing the exact bug #1461 fixed,
in the one place the pool-level fix cannot reach. As of this writing, the six migrations added
alongside #1461/#1464 follow-up work (`000123`-`000128`) only use `NOW()` for `updated_at`, whose
absolute value is timezone-independent — this hazard is currently latent, not yet triggered, but
the next migration that computes a deadline or backfills a calendar-relative column will hit it if
this isn't caught in review.

Expressions that are session-time-zone-dependent and therefore unsafe unpinned in a migration:

- **`make_interval(...)` added to or subtracted from a `timestamptz`** — its **calendar** components
  (`years`, `months`, `weeks`, `days`) are evaluated in the session zone before converting back to
  an absolute instant. Its **fixed-duration** components (`hours`, `mins`, `secs`) are exact and
  are *not* session-dependent — so `make_interval(hours => 72)` is safe unpinned while
  `make_interval(months => 6)` is not. Getting this distinction wrong in either direction wastes a
  review round: the repo has several `INTERVAL '<n> hours'` / `'<n> seconds'` sites that are
  genuinely unaffected.
- **`INTERVAL 'N days'` (or any calendar unit) added to a `timestamptz`** — the *days* component is
  applied as calendar days in the session zone, preserving local wall-clock time rather than adding
  exactly 24h × N. This is easy to miss because it looks like pure duration arithmetic; it isn't,
  once the interval carries a calendar unit rather than a fixed-duration one.
- **`CURRENT_DATE`** — resolves to "today" in the session zone, which can be a different calendar
  date than "today" in UTC near a day boundary.

**Squawk cannot detect this**, for the same reason it misses the DDL-plus-backfill hazard above: it
lints individual SQL statements, not the session context a migration transaction executes under.
Review any migration that does calendar arithmetic on a `timestamptz` by eye, and wrap the
time-zone-sensitive expression in `AT TIME ZONE 'UTC'` (or compute the value in application code
under the pinned pool, if the migration can be restructured to do so) rather than trusting the
gates to catch an unpinned session zone.

### The complete inventory of session-TZ-dependent SQL (Issue #1461)

Recorded here because it previously existed only in a commit message and four inline comments — and
because the **first** attempt at this inventory undercounted it as "two lines", which is how four of
these sites shipped unguarded.

**Ten sites, in three families.** All are deterministic today *only* because `NewPool` pins every
pooled connection to UTC (`backend/internal/database/pgx.go`); none of them is safe if that pin is
lost, and none of them is reached by the migration paths described above.

| Family | Count | Where |
|---|---|---|
| `CURRENT_DATE` written into a `DATE` column | 2 | `savings_contribution_repository.go` |
| `clock_timestamp() + make_interval(months => N)` on a `timestamptz` | 4 | `profile_repository.go` ×3, `admin_user_repository.go` ×1 |
| `timestamptz - INTERVAL '<n> days'` (the 30-day and 7-day GDPR pre-deletion notice boundaries) | 4 | `profile_repository.go` |

**The distinction that decides membership**, and the one the first sweep got wrong: an interval's
**calendar** components (`years`, `months`, `weeks`, `days`) are applied in the session zone, so day
arithmetic preserves local wall-clock time rather than adding exactly 24h. Its **fixed-duration**
components (`hours`, `mins`, `secs`) are exact and are *not* session-dependent. So
`INTERVAL '30 days'` belongs on this list and `INTERVAL '72 hours'` does not — and the repo has
several exact-unit interval sites that are genuinely unaffected. Conflating the two wastes a review
round in either direction.

Everything else checked and found safe: all 20 `AT TIME ZONE` sites already pin `'UTC'` explicitly;
every `DATE_TRUNC` operates on a `DATE` column; `::date` casts are either on `DATE` columns or on
explicitly-typed parameters; there are no `CURRENT_TIME` uses.

## Rollback Patterns (.down.sql)

### Drop Table

```sql
-- 000016_add_feature_flags.down.sql

DROP TABLE IF EXISTS feature_flags CASCADE;
```

### Drop Column

```sql
ALTER TABLE users DROP COLUMN IF EXISTS preferences;
```

### Drop Constraint

```sql
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS chk_amount_positive;
```

### Drop Index

```sql
DROP INDEX IF EXISTS idx_transactions_date;
```

### Remove Enum Value

```sql
-- Note: PostgreSQL doesn't easily support removing enum values
-- Document this limitation in the down migration
-- Option 1: Recreate enum (requires table recreation)
-- Option 2: Leave enum value (recommended for safety)
```

## golang-migrate + Squawk Commands

### Lint Migrations (ALWAYS before commit!)

```bash
cd backend
squawk migrations/*.sql --pg-version=17
```

**Squawk catches:**
- Dangerous DROP TABLE/COLUMN operations
- NOT NULL columns without defaults
- Non-concurrent index creation
- Type changes that require rewrites

### Apply Migrations

```bash
# Local development
cd backend
export DATABASE_URL="postgres://user:pass@localhost:5432/dbname?sslmode=disable"
migrate -database "$DATABASE_URL" -path migrations up

# Apply specific number
migrate -database "$DATABASE_URL" -path migrations up 3
```

### Check Migration Status

```bash
migrate -database "$DATABASE_URL" -path migrations version
```

### Rollback Migrations

```bash
# Rollback last migration
migrate -database "$DATABASE_URL" -path migrations down 1

# Rollback all migrations
migrate -database "$DATABASE_URL" -path migrations down -all
```

### Force Version (Recovery)

```bash
# If a migration fails mid-execution and leaves dirty state
migrate -database "$DATABASE_URL" -path migrations force <version>
```

## Migration Testing

### Run Full Test Suite

```bash
cd backend
./scripts/test-migrations.sh
```

### Test Suites

The script runs 5 test suites:

| Suite | Purpose |
|-------|---------|
| **Idempotency** | Run migrations twice, verify no errors |
| **Performance** | Track migration duration |
| **Data Integrity** | Verify existing data preserved |
| **Schema Validation** | Check tables, constraints, indexes |
| **Rollback** | Test up→down→up produces same schema |

### Manual Testing

```bash
# 1. Start fresh database
docker-compose down -v
docker-compose up -d postgres

# 2. Run migrations
migrate -database "$DATABASE_URL" -path migrations up

# 3. Verify schema
psql "$DATABASE_URL" -c "\d feature_flags"

# 4. Test rollback
migrate -database "$DATABASE_URL" -path migrations down 1

# 5. Re-apply
migrate -database "$DATABASE_URL" -path migrations up
```

## Common Issues

### Issue: Non-Idempotent Migration

**Symptom:** Migration fails on re-run with "already exists" error

**Fix:** Add appropriate IF NOT EXISTS guards

```sql
-- Bad
ALTER TABLE users ADD COLUMN theme VARCHAR(20);

-- Good
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'theme') THEN
        ALTER TABLE users ADD COLUMN theme VARCHAR(20);
    END IF;
END $$;
```

### Issue: Dirty Migration State

**Symptom:** "Dirty database version N" error

**Fix:** Check database state and force version if needed

```bash
# Check state
psql "$DATABASE_URL" -c "SELECT version, dirty FROM schema_migrations"

# If dirty=true, investigate and fix, then force version
migrate -database "$DATABASE_URL" -path migrations force <version>
```

### Issue: Missing DOWN Migration

**Symptom:** Rollback fails

**Fix:** Create .down.sql that reverses .up.sql

### Issue: Foreign Key Constraint

**Symptom:** "cannot drop table" error during rollback

**Fix:** Use CASCADE

```sql
DROP TABLE IF EXISTS feature_flags CASCADE;
```

### Issue: Data Loss on Rollback

**Symptom:** Important data deleted by rollback

**Fix:** Consider data migration approach

```sql
-- down.sql with data preservation
-- Store data before dropping
CREATE TABLE IF NOT EXISTS feature_flags_backup AS SELECT * FROM feature_flags;
DROP TABLE IF EXISTS feature_flags CASCADE;
```

## Deployment Behavior

### Container Startup

Migrations run automatically when the backend container starts via `docker-entrypoint.sh`:

1. Transform Neon pooler URL to direct connection
2. Run migrations with retry logic (5 attempts)
3. Start application

### Idempotency Requirement

**Why migrations MUST be idempotent:**

1. Container restarts re-run migrations
2. Multiple containers may start simultaneously
3. Failed deploys may leave partial state
4. Rollbacks require re-application

## Checklist

Before committing a migration:

- [ ] `.up.sql` uses idempotency patterns
- [ ] `.down.sql` correctly reverses changes
- [ ] `squawk migrations/*.sql --pg-version=17` passes
- [ ] `./scripts/test-migrations.sh` passes all 5 suites
- [ ] No sensitive data in migrations
- [ ] Rollback tested manually

## Related

- `.claude/commands/db-migrate.md` - Create migration command
- `.claude/agents/db-migrator.md` - Migration subagent
- `backend/docs/golang-migrate-guide.md` - Extended documentation
- `backend/docs/migration-testing.md` - Testing documentation
