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
# PgBouncer URL pattern: postgresql://user:pass@pgbouncer.<cluster-id>.flympg.net/fly-db
# Direct URL pattern:    postgresql://user:pass@boostedbudget-db.flycast:5432/fly-db
#
# Atlas is incompatible with PgBouncer transaction pooling mode because it tries
# to set connection parameters (like search_path) that PgBouncer blocks.
# Solution: Use direct PostgreSQL connection for migrations only.
MIGRATION_URL=$(echo "$DATABASE_URL" | sed 's/pgbouncer\.[a-z0-9]*\.flympg\.net/boostedbudget-db.flycast:5432/')

echo "Using direct PostgreSQL connection for migrations (bypassing PgBouncer)"

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
