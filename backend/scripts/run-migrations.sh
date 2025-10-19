#!/bin/bash
set -e

echo "========================================="
echo "Running Database Migrations"
echo "========================================="

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

echo "Migration tool found"

# Run migrations
echo "Executing migrations..."
migrate -path /app/migrations -database "$DATABASE_URL" -verbose up

# Check exit code
if [ $? -eq 0 ]; then
  echo "========================================="
  echo "✅ Migrations completed successfully"
  echo "========================================="
  exit 0
else
  echo "========================================="
  echo "❌ Migration failed"
  echo "========================================="
  exit 1
fi
