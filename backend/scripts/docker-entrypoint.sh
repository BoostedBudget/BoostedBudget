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

# Run migrations using Atlas
# --dir specifies the directory containing migration files
# --url uses the DATABASE_URL environment variable
# Atlas automatically detects and applies pending migrations
atlas migrate apply \
  --dir file:///app/migrations \
  --url "$DATABASE_URL"

if [ $? -eq 0 ]; then
  echo "✓ Migrations completed successfully"
else
  echo "✗ Migration failed"
  echo "Run 'atlas migrate status' for details"
  exit 1
fi

echo "Starting server..."
exec /app/server
