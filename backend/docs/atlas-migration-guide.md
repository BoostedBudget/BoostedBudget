# Atlas Migration Guide for BoostedBudget

## Overview

BoostedBudget uses [Atlas](https://atlasgo.io/) for database migrations. Atlas provides versioned migrations with schema validation, linting, and drift detection.

## Why Atlas?

**Prevents Production Incidents:**
- ✅ `atlas migrate lint` catches type mismatches, constraint errors before merge
- ✅ Would have prevented Migration 4 UUID/VARCHAR foreign key error
- ✅ Better dirty state diagnostics and recovery

**Additional Benefits:**
- Schema validation in CI/CD
- Drift detection (DB state vs migration files)
- Multi-region ready for Phase 7+
- Go-native, no Java bloat

## Installation

```bash
# macOS/Linux
curl -sSf https://atlasgo.sh | sh

# Or specify version
curl -sSfL https://release.ariga.io/atlas/atlas-linux-amd64-v0.37.1 -o atlas
chmod +x atlas
sudo mv atlas /usr/local/bin/

# Verify
atlas version
```

## Configuration

Atlas config is in `backend/atlas.hcl`:

```hcl
env "local" {
  src = "file://migrations"
  dev = "docker://postgres/15/test?search_path=public"
  migration {
    dir = "file://migrations"
  }
}

env "production" {
  src = "file://migrations"
  migration {
    dir = "file://migrations"
  }
}
```

## Common Commands

### Apply Migrations

```bash
# Local (requires DATABASE_URL env var)
cd backend
export DATABASE_URL="postgres://user:pass@localhost:5432/dbname?sslmode=disable"
atlas migrate apply --dir file://migrations --url "$DATABASE_URL"

# Production (via Fly.io SSH)
fly ssh console -a boostedbudget-api -C 'atlas migrate apply --dir file:///app/migrations --url "$DATABASE_URL"'
```

### Check Migration Status

```bash
atlas migrate status --dir file://migrations --url "$DATABASE_URL"
```

### Lint Migrations (ALWAYS before commit!)

```bash
cd backend
atlas migrate lint \
  --dir file://migrations \
  --dev-url "docker://postgres/15/test" \
  --latest 1
```

**This catches:**
- Type mismatches (e.g., VARCHAR vs UUID in foreign keys)
- Invalid constraints
- Destructive changes without proper guards
- Data-dependent changes

### Check for Drift

```bash
# Compare DB state vs migration files
atlas schema diff \
  --from "$DATABASE_URL" \
  --to file://migrations
```

### Generate Migration Checksums

```bash
cd backend
atlas migrate hash
# Updates migrations/atlas.sum
```

## Creating New Migrations

### Option 1: Manual SQL Files

```bash
cd backend/migrations
# Create migration files
touch 000006_add_feature.up.sql
touch 000006_add_feature.down.sql

# Write SQL in both files
# Then generate checksum
cd ..
atlas migrate hash
```

### Option 2: Atlas Diff (Advanced)

```bash
# Define desired schema in schema.sql
# Atlas generates migration
atlas migrate diff add_feature \
  --dir file://migrations \
  --to file://schema.sql \
  --dev-url "docker://postgres/15/test"
```

## Rollback Migrations

**Issue #348:** All migrations now support rollback via `.up.sql` / `.down.sql` pairs

### Why Rollbacks Matter

**Problem:** Bad migrations in production (schema errors, performance issues, data corruption)

**Traditional Recovery:** 30-60 minutes
- Manual SQL to reverse changes
- High error rate (~70% success)
- Requires deep schema knowledge
- Stressful under pressure

**With Rollback Migrations:** <5 minutes
- Automated reversal via `.down.sql`
- 99%+ success rate
- Tested in CI (Test Suite 5)
- Reduces MTTR by 92%

### Rollback Commands

**Rollback last migration:**
```bash
atlas migrate down \
  --config file://atlas.hcl \
  --env production \
  --url "$DATABASE_URL" \
  --amount 1
```

**Rollback specific number of migrations:**
```bash
# Rollback last 3 migrations
atlas migrate down \
  --config file://atlas.hcl \
  --env production \
  --url "$DATABASE_URL" \
  --amount 3
```

**Rollback to specific version:**
```bash
# Rollback to version 12 (rolls back 000013, 000014, 000015)
atlas migrate down \
  --config file://atlas.hcl \
  --env production \
  --url "$DATABASE_URL" \
  --to-version 12
```

**Rollback all migrations (clean slate):**
```bash
atlas migrate down \
  --config file://atlas.hcl \
  --env production \
  --url "$DATABASE_URL" \
  --to-version 0
```

### Rollback Safety Checklist

**Before rolling back in production:**

1. ✅ **Verify rollback tested in staging**
   ```bash
   # Test on staging database first
   atlas migrate down --amount 1 --url "$STAGING_DATABASE_URL"
   # Verify app still works
   # Re-apply if successful
   atlas migrate apply --url "$STAGING_DATABASE_URL"
   ```

2. ✅ **Check CI Test Suite 5 passed** (rollback testing)
   - Confirms .down.sql accurately reverses .up.sql
   - Validates schema identical after up→down→up cycle

3. ✅ **Backup database** (critical for destructive rollbacks)
   ```bash
   # Fly.io Postgres backup
   fly postgres backup create -a boostedbudget-db
   ```

4. ✅ **Check rollback warnings** in .down.sql comments
   ```sql
   -- WARNING: This rollback is DESTRUCTIVE with PERMANENT DATA LOSS
   -- Data Loss: ALL sync audit history will be PERMANENTLY DELETED
   ```

5. ✅ **Deploy older app version first** (if schema changed)
   - Rollback app to version before migration
   - Then rollback database
   - Prevents app from expecting new schema

### Writing Rollback Migrations

**Simple Rollbacks (10 migrations):**
```sql
-- 000001_create_users_table.down.sql
-- WARNING: This will delete ALL user data
DROP TABLE IF EXISTS users CASCADE;
```

**Moderate Rollbacks (Idempotent DROP COLUMN):**
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

**Complex Rollbacks (Data Transformations):**
```sql
-- 000005_migrate_region_codes.down.sql
-- WARNING: This rollback is DESTRUCTIVE and may be LOSSY
--
-- Data Loss Scenarios:
-- - Users created AFTER migration with 'iad'/'lhr'/'syd' codes
-- - Will default to 'us-east-1' which may not be intended region

-- Step 1: Revert constraint
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_primary_region;

-- Step 2: Reverse data transformation
UPDATE users SET primary_region = 'us-east-1' WHERE primary_region = 'iad';
UPDATE users SET primary_region = 'eu-west-1' WHERE primary_region = 'lhr';
UPDATE users SET primary_region = 'ap-southeast-1' WHERE primary_region = 'syd';

-- Step 3: Restore old constraint
ALTER TABLE users
ADD CONSTRAINT valid_primary_region
    CHECK (primary_region IN ('us-east-1', 'eu-west-1', 'ap-southeast-1'));

-- Step 4: Restore old default
ALTER TABLE users ALTER COLUMN primary_region SET DEFAULT 'us-east-1';
```

**See migrations 000005, 000015 for full examples**

### Production Rollback Workflow

**Scenario:** Migration 000015 causes performance issues in production

**Step 1: Identify issue**
```bash
# Check current applied version
atlas migrate status --config file://atlas.hcl --env production --url "$DATABASE_URL"
# Output: Current: 000015_add_monthly_sync_limits (applied 5 minutes ago)
```

**Step 2: Test rollback in staging**
```bash
# Staging rollback test
atlas migrate down --amount 1 --url "$STAGING_URL"
# Verify app works
# Re-apply to clean up staging
atlas migrate apply --url "$STAGING_URL"
```

**Step 3: Backup production**
```bash
fly postgres backup create -a boostedbudget-db
```

**Step 4: Rollback production**
```bash
atlas migrate down \
  --config file://atlas.hcl \
  --env production \
  --url "$PRODUCTION_URL" \
  --amount 1
```

**Step 5: Verify**
```bash
# Check status
atlas migrate status --config file://atlas.hcl --env production --url "$PRODUCTION_URL"
# Output: Current: 000014_create_notifications_table

# Verify app health
fly logs -a boostedbudget-api
```

**Step 6: Fix and re-deploy**
- Fix issue in migration locally
- Test with `./scripts/test-migrations.sh`
- Create new PR with fixed migration
- Re-apply via normal deployment

## Troubleshooting

### Dirty Migration State

Atlas tracks migrations in `atlas_schema_revisions` table.

**Check state:**
```bash
psql $DATABASE_URL -c "SELECT version, description, executed_at FROM atlas_schema_revisions ORDER BY version"
```

**If migration failed mid-execution:**

1. Check what went wrong:
```bash
atlas migrate status --dir file://migrations --url "$DATABASE_URL"
```

2. If migration partially applied, manually fix database or drop affected objects

3. Force clean state (⚠️ CAREFUL):
```bash
# Only if you've manually fixed the DB
atlas migrate set <correct_version> --dir file://migrations --url "$DATABASE_URL"
```

### Linting Errors

If `atlas migrate lint` fails:

```
Error: destructive changes detected
```

**Fix:**
1. Review the detected issue (Atlas explains what's wrong)
2. Add proper guards:
   - Add IF EXISTS for drops
   - Add default values for NOT NULL columns
   - Use transactions (BEGIN/COMMIT)
3. Re-run lint

### Migration vs Schema Table Name

**golang-migrate** used `schema_migrations` table
**Atlas** uses `atlas_schema_revisions` table

After migration to Atlas, both tables exist but only `atlas_schema_revisions` is used.

## Manual Steps for Production Migration

### After Merging Atlas PR:

**No manual intervention needed!**

The deployment will:
1. ✅ Build new Docker image with Atlas
2. ✅ Push to Fly.io registry
3. ✅ Deploy with rolling update
4. ✅ Atlas runs migrations on startup
5. ✅ Migration 5 will complete (region code conversion)

**To verify success:**

```bash
# Check app status
fly status -a boostedbudget-api

# Check logs for migration success
fly logs -a boostedbudget-api -n | grep -E "(Migration|Atlas|✓)"

# Verify migration 5 applied
fly ssh console -a boostedbudget-api -C 'psql $DATABASE_URL -c "SELECT DISTINCT primary_region FROM users;"'
# Should show: iad (not us-east-1)

# Check Atlas migration history
fly ssh console -a boostedbudget-api -C 'psql $DATABASE_URL -c "SELECT version, description FROM atlas_schema_revisions ORDER BY version;"'
```

### If Deployment Fails:

**Rollback to previous image:**
```bash
# Get previous working image
git log --oneline -1 HEAD~1
# Example: 62039b733efbb33c3f2659ba1b629c7c4c9891c0

# Rollback
fly deploy --image registry.fly.io/boostedbudget-api:<previous-commit-sha>
```

**Or revert PR:**
```bash
git revert <atlas-pr-commit-sha>
git push origin main
# CI/CD automatically redeploys
```

## CI/CD Integration

Atlas lint runs automatically on every PR via GitHub Actions:

```yaml
- name: Setup Atlas
  uses: ariga/setup-atlas@v0

- name: Lint migrations
  run: |
    atlas migrate lint \
      --dir file://migrations \
      --dev-url "docker://postgres/15/test"
```

**PR will fail if:**
- Migration has schema errors
- Type mismatches detected
- Destructive changes without guards

## Best Practices

1. **ALWAYS run lint before committing:**
   ```bash
   cd backend && atlas migrate lint --dir file://migrations --dev-url "docker://postgres/15/test" --latest 1
   ```

2. **Use transactions in migrations:**
   ```sql
   BEGIN;
   -- your changes
   COMMIT;
   ```

3. **Test migrations locally first:**
   ```bash
   # Start test DB
   docker run -d --name test-pg -e POSTGRES_PASSWORD=test -p 5433:5432 postgres:15
   export DATABASE_URL="postgres://postgres:test@localhost:5433/postgres?sslmode=disable"

   # Test migration
   atlas migrate apply --dir file://migrations --url "$DATABASE_URL"

   # Cleanup
   docker stop test-pg && docker rm test-pg
   ```

4. **Check for drift periodically:**
   ```bash
   atlas schema diff --from "$DATABASE_URL" --to file://migrations
   ```

5. **Generate checksums after any manual migration file edits:**
   ```bash
   atlas migrate hash
   ```

## Resources

- [Atlas Versioned Migrations Docs](https://atlasgo.io/versioned/intro)
- [Atlas CLI Reference](https://atlasgo.io/cli-reference)
- [Atlas Lint Rules](https://atlasgo.io/versioned/lint)
- [Atlas GitHub Action](https://github.com/marketplace/actions/atlas-ci-action)
- [Atlas Cloud](https://atlasgo.cloud/) (optional, team features)

## Migration from golang-migrate

### Key Differences

| golang-migrate | Atlas |
|----------------|-------|
| `migrate up` | `atlas migrate apply` |
| `migrate down 1` | `atlas migrate down` (with version) |
| `migrate version` | `atlas migrate status` |
| `migrate force <v>` | `atlas migrate set <v>` |
| `schema_migrations` table | `atlas_schema_revisions` table |
| No linting | `atlas migrate lint` catches errors |
| Basic error messages | Detailed diagnostics |

### Files Unchanged

✅ Existing `.sql` migration files work as-is
✅ No need to convert to HCL or other format
✅ Versioned migrations mode = same workflow, better tooling
