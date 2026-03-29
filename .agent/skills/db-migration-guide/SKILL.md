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
