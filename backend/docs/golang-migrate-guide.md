# golang-migrate Guide for BoostedBudget

## Overview

BoostedBudget uses [golang-migrate](https://github.com/golang-migrate/migrate) for database migrations and [Squawk](https://squawkhq.com/) for PostgreSQL migration linting.

**Issue #631:** Migrated from Atlas to golang-migrate + Squawk to avoid vendor lock-in.

## Why golang-migrate + Squawk?

**golang-migrate:**
- Simple, focused migration tool
- Open source with active community
- No vendor lock-in or paid features
- Supports `.up.sql/.down.sql` file format
- 17.9k GitHub stars, battle-tested

**Squawk:**
- PostgreSQL-specific linting
- Catches dangerous migration patterns
- Free and open source
- Configurable rules via `.squawk.toml`

## Installation

### golang-migrate

```bash
# macOS (Homebrew)
brew install golang-migrate

# Linux
curl -sSfL https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar -xz
sudo mv migrate /usr/local/bin/

# Go install
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Verify
migrate --version
```

### Squawk

```bash
# npm (recommended)
npm install -g squawk-cli

# Homebrew
brew install squawk

# Verify
squawk --version
```

## Configuration

### Squawk Configuration (`.squawk.toml`)

Located in project root:

```toml
# Target PostgreSQL version (matches Neon production)
pg_version = "17"

# Rules to exclude
excluded_rules = [
  "prefer-big-int-over-serial",  # We use gen_random_uuid()
  "require-concurrent-index-creation",  # Small tables initially
]
```

## Common Commands

### Apply Migrations

```bash
# Local (requires DATABASE_URL env var)
cd backend
export DATABASE_URL="postgres://user:pass@localhost:5432/dbname?sslmode=disable"
migrate -database "$DATABASE_URL" -path migrations up

# Apply specific number
migrate -database "$DATABASE_URL" -path migrations up 3
```

### Check Current Version

```bash
migrate -database "$DATABASE_URL" -path migrations version
```

### Lint Migrations (ALWAYS before commit!)

```bash
cd backend
squawk migrations/*.sql --pg-version=17
```

**Squawk catches:** DROP TABLE/COLUMN, NOT NULL without defaults, non-concurrent indexes, type rewrites.

### Rollback Migrations

```bash
# Rollback last migration
migrate -database "$DATABASE_URL" -path migrations down 1

# Rollback specific number
migrate -database "$DATABASE_URL" -path migrations down 3

# Rollback ALL migrations
migrate -database "$DATABASE_URL" -path migrations down -all
```

### Force Version (Recovery)

```bash
# If a migration fails mid-execution and leaves dirty state
migrate -database "$DATABASE_URL" -path migrations force <version>
```

## Creating New Migrations

### Step 1: Determine Next Version Number

```bash
cd backend/migrations
ls -1 *.up.sql | tail -1
# Shows: 000030_add_river_queue.up.sql
# Next number: 000031
```

### Step 2: Create Migration Files

```bash
touch 000031_your_feature.up.sql
touch 000031_your_feature.down.sql
```

### Step 3: Write Idempotent SQL

**up.sql:**
```sql
-- 000031_your_feature.up.sql
CREATE TABLE IF NOT EXISTS your_table (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_your_table_name ON your_table(name);
```

**down.sql:**
```sql
-- 000031_your_feature.down.sql
DROP TABLE IF EXISTS your_table CASCADE;
```

### Step 4: Lint and Test

```bash
# Lint
squawk migrations/000031_*.sql --pg-version=17

# Run full test suite
./scripts/test-migrations.sh
```

## Rollback Guide

**Issue #348:** All migrations support rollback via `.up.sql` / `.down.sql` pairs

### Why Rollbacks Matter

| Without Rollbacks | With Rollbacks |
|-------------------|----------------|
| 30-60 min recovery | <5 min recovery |
| ~70% success rate | 99%+ success rate |
| Manual SQL fixes | Automated reversal |
| High stress | Low stress |

### Rollback Commands

```bash
# Rollback last migration
migrate -database "$DATABASE_URL" -path migrations down 1

# Rollback to clean state (all migrations)
migrate -database "$DATABASE_URL" -path migrations down -all

# Check current state
migrate -database "$DATABASE_URL" -path migrations version
```

### Rollback Safety Checklist

Before rolling back in production:

1. **Test in staging first**
2. **Verify Test Suite 5 passed** (rollback testing in CI)
3. **Backup database** before destructive rollbacks
4. **Check .down.sql comments** for data loss warnings
5. **Deploy older app version first** if schema changed

### Writing Rollback Migrations

**Simple (DROP TABLE):**
```sql
-- 000001_create_users_table.down.sql
DROP TABLE IF EXISTS users CASCADE;
```

**With Column Check (Idempotent):**
```sql
-- 000009_add_provider_to_linked_items.down.sql
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'provider'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN provider;
    END IF;
END $$;
```

**Complex (Data Transformations):**
```sql
-- 000005_migrate_region_codes.down.sql
-- WARNING: DESTRUCTIVE - may lose data for users created after migration

-- Revert constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Reverse data transformation
UPDATE users SET primary_region = 'us-east-1' WHERE primary_region = 'iad';
UPDATE users SET primary_region = 'eu-west-1' WHERE primary_region = 'lhr';

-- Restore old constraint
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('us-east-1', 'eu-west-1', 'ap-southeast-1'));
```

## Troubleshooting

### Dirty Migration State

golang-migrate tracks state in `schema_migrations` table.

**Check state:**
```bash
psql "$DATABASE_URL" -c "SELECT version, dirty FROM schema_migrations"
```

**If dirty=true (migration failed mid-execution):**

1. Investigate what went wrong
2. Manually fix database if needed
3. Force version:
```bash
migrate -database "$DATABASE_URL" -path migrations force <version>
```

### Squawk Linting Errors

If `squawk` reports violations:

1. Review the rule documentation at https://squawkhq.com/docs/rules
2. Fix the SQL or disable the rule in `.squawk.toml` if appropriate
3. Re-run linting

**Common fixes:**
- Add `IF EXISTS` for drops
- Add default values for NOT NULL columns
- Use `CONCURRENTLY` for indexes on large tables

### Connection Pooler Issues

golang-migrate requires direct PostgreSQL connections (not PgBouncer/Neon pooler).

**Solution:** Use `MIGRATION_DATABASE_URL` env var for direct connection:
```bash
# Neon: Use direct endpoint (without -pooler in hostname)
export MIGRATION_DATABASE_URL="postgres://user:pass@ep-xxx.us-east-1.aws.neon.tech/dbname?sslmode=require"
```

## CI/CD Integration

Migrations are tested and linted automatically on every PR:

```yaml
# .github/workflows/test.yml
- name: Lint migrations with Squawk
  run: squawk migrations/*.sql --pg-version=17

- name: Run migration tests
  run: ./scripts/test-migrations.sh
```

**PR will fail if:**
- Squawk detects unsafe patterns
- Any of the 5 test suites fail

## Best Practices

1. **ALWAYS run Squawk before committing:**
   ```bash
   squawk migrations/*.sql --pg-version=17
   ```

2. **ALWAYS run test suite:**
   ```bash
   ./scripts/test-migrations.sh
   ```

3. **Use idempotent patterns:**
   - `CREATE TABLE IF NOT EXISTS`
   - `CREATE INDEX IF NOT EXISTS`
   - `DO $$ BEGIN IF NOT EXISTS ... END IF; END $$;`

4. **Test locally first:**
   ```bash
   docker run -d --name test-pg -e POSTGRES_PASSWORD=test -p 5433:5432 postgres:17
   export DATABASE_URL="postgres://postgres:test@localhost:5433/postgres?sslmode=disable"
   migrate -database "$DATABASE_URL" -path migrations up
   docker stop test-pg && docker rm test-pg
   ```

5. **Write both .up.sql and .down.sql** for every migration

## Command Reference

| Operation | Command |
|-----------|---------|
| Apply all | `migrate -database "$DB_URL" -path migrations up` |
| Apply N | `migrate -database "$DB_URL" -path migrations up N` |
| Rollback 1 | `migrate -database "$DB_URL" -path migrations down 1` |
| Rollback all | `migrate -database "$DB_URL" -path migrations down -all` |
| Check version | `migrate -database "$DB_URL" -path migrations version` |
| Force version | `migrate -database "$DB_URL" -path migrations force N` |
| Lint | `squawk migrations/*.sql --pg-version=17` |

## Resources

- [golang-migrate Documentation](https://github.com/golang-migrate/migrate)
- [golang-migrate CLI Reference](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)
- [Squawk Documentation](https://squawkhq.com/docs)
- [Squawk Rules Reference](https://squawkhq.com/docs/rules)
