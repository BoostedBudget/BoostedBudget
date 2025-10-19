#!/bin/bash
set -e

echo "Starting BoostedBudget Backend..."

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL environment variable is not set"
  exit 1
fi

echo "Database URL configured"
echo "Note: Database migrations are run via CI/CD before deployment"
echo "      This prevents crash loops from failed migrations"

echo "Starting server..."
exec /app/server
