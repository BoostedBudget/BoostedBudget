# Database Migration

Create a new migration file following Atlas/idempotency patterns.

## Usage

```
/db-migrate [description]
```

Example: `/db-migrate add-user-preferences-table`

## Workflow

### 1. Determine Next Migration Number

```bash
ls backend/migrations/*.up.sql | wc -l
# Or find highest number:
ls backend/migrations/*.up.sql | sort -V | tail -1
```

### 2. Create Migration Files

```bash
# Replace XXX with next number (e.g., 000016)
touch backend/migrations/000XXX_description.up.sql
touch backend/migrations/000XXX_description.down.sql
```

### 3. Write .up.sql (with Idempotency)

**Creating Tables:**
```sql
-- 000016_add_user_preferences.up.sql

CREATE TABLE IF NOT EXISTS user_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    preference_key VARCHAR(100) NOT NULL,
    preference_value JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_preference UNIQUE (user_id, preference_key)
);

CREATE INDEX IF NOT EXISTS idx_user_preferences_user_id ON user_preferences(user_id);
```

**Adding Columns:**
```sql
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'users' AND column_name = 'theme'
    ) THEN
        ALTER TABLE users ADD COLUMN theme VARCHAR(20) DEFAULT 'light';
    END IF;
END $$;
```

**Adding Constraints:**
```sql
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_theme_valid'
    ) THEN
        ALTER TABLE users
        ADD CONSTRAINT chk_theme_valid
        CHECK (theme IN ('light', 'dark', 'system'));
    END IF;
END $$;
```

### 4. Write .down.sql (Rollback)

```sql
-- 000016_add_user_preferences.down.sql

DROP TABLE IF EXISTS user_preferences CASCADE;

-- For column additions:
-- ALTER TABLE users DROP COLUMN IF EXISTS theme;
```

### 5. Regenerate Atlas Checksums

```bash
cd backend
atlas migrate hash --dir file://migrations --dir-format golang-migrate
```

### 6. Lint Migration

```bash
cd backend
atlas migrate lint --dir file://migrations --dev-url "docker://postgres/15/test" --latest 1
```

### 7. Run Migration Tests

```bash
cd backend
./scripts/test-migrations.sh
```

**5 Test Suites:**
1. Idempotency - Can run twice safely
2. Performance - Duration tracking
3. Data Integrity - Preserves existing data
4. Schema Validation - Tables/constraints correct
5. Rollback Testing - up→down→up produces identical schema

### 8. Verify Locally

```bash
# Start local database
docker-compose up -d postgres

# Run migrations
cd backend
atlas migrate apply --dir file://migrations --url "$DATABASE_URL"

# Verify schema
psql "$DATABASE_URL" -c "\d user_preferences"
```

## Idempotency Patterns Reference

| Operation | Pattern |
|-----------|---------|
| Create table | `CREATE TABLE IF NOT EXISTS` |
| Create index | `CREATE INDEX IF NOT EXISTS` |
| Add column | `DO $$ IF NOT EXISTS... END $$` |
| Add constraint | `DO $$ IF NOT EXISTS... END $$` |
| Drop | `DROP ... IF EXISTS ... CASCADE` |

## Common Issues

**Non-idempotent migration:**
- Causes: Missing `IF NOT EXISTS`
- Fix: Add idempotency guards

**Checksum mismatch:**
- Causes: Modified migration after hash
- Fix: `atlas migrate hash --dir file://migrations --dir-format golang-migrate`

**Foreign key errors:**
- Causes: Missing `ON DELETE CASCADE`
- Fix: Add cascade to foreign keys

## Checklist

- [ ] .up.sql with idempotency guards
- [ ] .down.sql correctly reverses changes
- [ ] `atlas migrate hash` completed
- [ ] `atlas migrate lint` passes
- [ ] `./scripts/test-migrations.sh` passes
- [ ] Local verification successful

## Related

- `.claude/skills/db-migration-guide/` - Complete migration reference
- `.claude/agents/db-migrator.md` - Migration subagent
