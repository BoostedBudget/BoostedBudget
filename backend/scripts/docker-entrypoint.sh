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

# Add search_path=public to connection URL to avoid "database not clean" error from pgbouncer schema
# This tells Atlas to only manage the public schema and ignore system schemas like pgbouncer
if [[ "$MIGRATION_URL" == *"?"* ]]; then
  MIGRATION_URL="${MIGRATION_URL}&search_path=public"
else
  MIGRATION_URL="${MIGRATION_URL}?search_path=public"
fi

# Run migrations using Atlas with direct connection
atlas migrate apply \
  --dir file:///app/migrations \
  --url "$MIGRATION_URL"

if [ $? -eq 0 ]; then
  echo "✓ Migrations completed successfully"
else
  echo "✗ Migration failed"
  echo "Run 'atlas migrate status' for details"
  exit 1
fi

echo "Starting server..."
exec /app/server
