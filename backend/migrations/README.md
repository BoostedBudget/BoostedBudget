# Database Migrations

This directory contains SQL migration files for BoostedBudget's PostgreSQL database.

## Migration Files

Migrations are numbered sequentially with the format: `NNNNNN_description.{up|down}.sql`

| Migration | Description |
|-----------|-------------|
| `000001` | Create users table with GDPR fields |
| `000002` | Create privacy_settings table |
| `000003` | Create audit_logs table |

## Running Migrations

### Production (Fly.io) - Automated

**Migrations run automatically on every deployment!**

When you deploy to Fly.io, the `docker-entrypoint.sh` script:
1. Checks for `DATABASE_URL` environment variable
2. Runs `migrate -path /app/migrations -database "$DATABASE_URL" up`
3. Applies any pending migrations
4. Starts the backend server

**Deployment flow:**
```bash
# Deploy backend (migrations run automatically)
flyctl deploy --app boostedbudget-api

# Check deployment logs to verify migrations
flyctl logs --app boostedbudget-api

# You should see:
# Starting BoostedBudget Backend...
# Database URL configured
# Running database migrations...
# ✓ Migrations completed successfully
# Starting server...
```

**Manual migration on Fly.io (if needed):**
```bash
# Option 1: Via proxy
flyctl mpg proxy  # Terminal 1
DATABASE_URL="postgresql://fly-user:PASSWORD@localhost:16380/fly-db" make migrate-up  # Terminal 2

# Option 2: Check migration status
flyctl ssh console --app boostedbudget-api -C "migrate -path /app/migrations -database \$DATABASE_URL version"
```

### Local Development

#### Prerequisites
- Docker Compose must be running (`docker-compose up` or `make dev`)
- PostgreSQL service must be healthy

#### Commands

```bash
# Apply all pending migrations
make migrate-up

# Rollback all migrations
make migrate-down

# Check current schema
make migrate-status
```

### Manual Execution

If you need to run migrations manually:

```bash
# Connect to database
docker-compose exec postgres psql -U devuser -d boostedbudget

# Apply a specific migration
docker-compose exec -T postgres psql -U devuser -d boostedbudget < backend/migrations/000001_create_users_table.up.sql

# Rollback a specific migration
docker-compose exec -T postgres psql -U devuser -d boostedbudget < backend/migrations/000001_create_users_table.down.sql
```

## Migration Structure

### 000001: Users Table

**Purpose**: Store user accounts with GDPR-compliant data residency

**Key Fields**:
- `auth0_user_id` - Auth0 identifier from JWT
- `email` - User email address
- `primary_region` - GDPR data residency region (default: 'us-east-1')
- `subscription_tier` - 'free', 'premium', or 'premium_plus'
- `subscription_status` - 'active', 'cancelled', 'past_due', 'unpaid'
- `deletion_requested_at` - When user requested account deletion
- `deletion_scheduled_at` - When account will be permanently deleted (30-day grace period)

**Indexes**:
- `auth0_user_id` (unique, for JWT lookups)
- `email` (unique, for user lookup)
- `primary_region` (for data residency queries)
- `subscription_tier` (for tier-based queries)
- `deletion_scheduled_at` (for cleanup jobs)

**Triggers**:
- Auto-update `updated_at` on row updates

### 000002: Privacy Settings Table

**Purpose**: GDPR-compliant user privacy and consent preferences

**Key Fields**:
- `user_id` - Foreign key to users table (cascade delete)
- `gdpr_necessary_consent` - Always true (required for app)
- `gdpr_analytics_consent` - Analytics cookies (Google Analytics)
- `gdpr_advertising_consent` - Advertising cookies (AdSense for free tier)
- `gdpr_consent_timestamp` - When consent last updated
- `marketing_emails_enabled` - Marketing email opt-in
- `product_updates_enabled` - Product update emails
- `security_alerts_enabled` - Security alert emails
- `data_sharing_enabled` - Share anonymized data

**Constraints**:
- One privacy settings record per user (unique constraint)
- Cascade delete when user is deleted

### 000003: Audit Logs Table

**Purpose**: GDPR-compliant audit trail of all data access and modifications

**Key Fields**:
- `user_id` - User who performed action (nullable, SET NULL on delete)
- `action` - Action performed (e.g., 'user.created', 'data.exported')
- `resource_type` - Type of resource ('user', 'account', 'budget', 'rule')
- `resource_id` - ID of affected resource
- `ip_address` - IP address of request
- `user_agent` - User agent string
- `request_id` - Request correlation ID
- `changes` - JSONB of before/after state for updates
- `status` - 'success', 'failure', or 'error'

**Indexes**:
- `user_id` + `created_at` (user activity timeline)
- `resource_type` + `resource_id` + `created_at` (resource history)
- `action` (filter by action type)
- `status` WHERE != 'success' (find failures)

**GDPR Compliance**:
- Retains logs for 7 years as required by GDPR
- User can request export of their audit logs
- User deletion sets `user_id` to NULL but retains logs

## GDPR Considerations

### Data Residency
- `users.primary_region` specifies where user data should be stored
- Default: 'us-east-1' (MVP single region)
- Future: 'eu-west-1', 'ap-southeast-1', etc.

### Right to Erasure
- User requests deletion via API
- Sets `deletion_requested_at` timestamp
- Sets `deletion_scheduled_at` to 30 days later (grace period)
- Cleanup job permanently deletes after grace period
- Privacy settings cascade delete immediately
- Audit logs retain with user_id set to NULL

### Right to Access
- User can request full data export
- Audit logs track all data exports
- Includes: user profile, privacy settings, accounts, budgets, rules, transactions

### Consent Management
- All GDPR consent tracked in `privacy_settings`
- Consent timestamp updated whenever preferences change
- Default to opt-out for non-essential cookies

## Testing Migrations

### Integration Tests

The migration tests are in `backend/internal/repository/migrations_test.go`:

```bash
# Run migration tests
go test ./internal/repository/... -v -run TestMigrations
```

### Manual Verification

```bash
# 1. Start fresh database
make docker-down && make docker-up

# 2. Apply migrations
make migrate-up

# 3. Verify tables exist
make migrate-status

# Expected output:
#  users
#  privacy_settings
#  audit_logs

# 4. Test rollback
make migrate-down

# 5. Verify tables removed
make migrate-status

# Expected output:
#  No tables found
```

## Rollback Safety

All migrations have corresponding down migrations:
- ✅ Safe to rollback `000003` (audit_logs)
- ✅ Safe to rollback `000002` (privacy_settings) - cascade deletes with user
- ⚠️  Rollback `000001` (users) - **DATA LOSS** - deletes all user data

**Production Rollback Protocol**:
1. Never rollback `000001` in production
2. Backup database before any rollback
3. Test rollback in staging first
4. Communicate downtime to users

## Future Migrations

When adding new migrations:

1. **Create migration files**:
   ```bash
   # Migration 000004 example
   touch backend/migrations/000004_create_accounts_table.up.sql
   touch backend/migrations/000004_create_accounts_table.down.sql
   ```

2. **Follow naming convention**:
   - Sequential numbering: `000001`, `000002`, `000003`, ...
   - Descriptive name: `create_table_name`, `add_column_to_table`, `create_index_on_table`
   - Both `.up.sql` and `.down.sql` files required

3. **Write SQL**:
   - Use `IF NOT EXISTS` for idempotency
   - Add comments explaining purpose
   - Create appropriate indexes
   - Add constraints for data integrity
   - Include `COMMENT ON` for documentation

4. **Test thoroughly**:
   - Test `make migrate-up`
   - Test `make migrate-down`
   - Verify data integrity
   - Check performance with indexes

5. **Update this README**:
   - Add migration to table
   - Document new tables/fields
   - Note any GDPR implications

---

**Last Updated**: 2025-10-18
**Issue**: #7
