#!/bin/bash
set -e

echo "Starting BoostedBudget Backend..."

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL environment variable is not set"
  exit 1
fi

echo "Database URL configured"

# Verify migrate tool is available
if ! command -v migrate &> /dev/null; then
  echo "ERROR: migrate tool not found in PATH"
  exit 1
fi

echo "Running database migrations..."

# Run migrations
# The -path flag specifies the directory containing migration files
# The -database flag uses the DATABASE_URL environment variable
# -verbose shows detailed output
migrate -path /app/migrations -database "$DATABASE_URL" -verbose up

if [ $? -eq 0 ]; then
  echo "✓ Migrations completed successfully"
else
  echo "✗ Migration failed"
  exit 1
fi

echo "Starting server..."
exec /app/server
