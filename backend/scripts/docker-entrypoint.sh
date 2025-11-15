#!/bin/bash
set -e

echo "Starting BoostedBudget Backend..."

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL environment variable is not set"
  exit 1
fi

echo "Database URL configured"

# Verify atlas tool is available
if ! command -v atlas &> /dev/null; then
  echo "ERROR: atlas tool not found in PATH"
  exit 1
fi

echo "Running database migrations with Atlas..."

# Construct direct PostgreSQL connection for Atlas (bypass PgBouncer)
# Atlas is incompatible with PgBouncer transaction pooling mode because it tries
# to set connection parameters (like search_path) that PgBouncer blocks.
# Solution: Use direct PostgreSQL connection for migrations only.
#
# Preferred: Use MIGRATION_DATABASE_URL env var if set (cleaner, explicit)
# Fallback: Transform DATABASE_URL from PgBouncer to direct connection
if [ -n "${MIGRATION_DATABASE_URL:-}" ]; then
  MIGRATION_URL="$MIGRATION_DATABASE_URL"
  echo "Using MIGRATION_DATABASE_URL for direct PostgreSQL connection"
else
  # Transform PgBouncer URL to direct connection
  # PgBouncer pattern: postgresql://user:pass@pgbouncer.<cluster-id>.flympg.net/fly-db
  # Direct pattern:    postgresql://user:pass@boostedbudget-db.internal:5432/fly-db
  MIGRATION_URL=$(echo "$DATABASE_URL" | sed 's/pgbouncer\.[a-z0-9]*\.flympg\.net/boostedbudget-db.internal:5432/')
  echo "Using transformed DATABASE_URL for direct PostgreSQL connection (bypassing PgBouncer)"
fi

# Run migrations using Atlas with direct connection
# Use --revisions-schema public to store revisions table in public schema
# This prevents Atlas from creating a separate atlas_schema_revisions schema
# and avoids conflicts with the pgbouncer schema
echo "🗄️  Running database migrations..."
MAX_ATTEMPTS=5
for i in $(seq 1 $MAX_ATTEMPTS); do
  echo "Migration attempt $i/$MAX_ATTEMPTS..."

  # Apply all pending migrations (Atlas handles fresh databases automatically)
  # Use atlas.hcl config for golang-migrate format support (.up.sql/.down.sql)
  atlas migrate apply \
    --config file:///app/atlas.hcl \
    --env production \
    --url "$MIGRATION_URL" \
    --revisions-schema public && {
    echo "✓ Migrations completed successfully"
    break
  } || {
    if [ $i -eq $MAX_ATTEMPTS ]; then
      echo "✗ Migration failed after $MAX_ATTEMPTS attempts"
      echo "Run 'atlas migrate status' for details"
      exit 1
    else
      echo "⚠ Migration attempt $i failed, retrying in 10s..."
      sleep 10
    fi
  }
done

echo "Starting server..."
exec /app/server
