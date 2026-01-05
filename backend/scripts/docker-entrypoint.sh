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

# Construct direct PostgreSQL connection for Atlas (bypass connection pooler)
# Atlas is incompatible with connection poolers (PgBouncer/Neon pooler) because
# it tries to set session-level parameters that poolers block.
# Solution: Use direct PostgreSQL connection for migrations only.
#
# Priority:
# 1. MIGRATION_DATABASE_URL if explicitly set (cleaner, most flexible)
# 2. Transform Neon pooler URL to direct endpoint
# 3. Use DATABASE_URL as-is (local/QA Docker PostgreSQL)

if [ -n "${MIGRATION_DATABASE_URL:-}" ]; then
  MIGRATION_URL="$MIGRATION_DATABASE_URL"
  echo "Using MIGRATION_DATABASE_URL for direct PostgreSQL connection"
elif [[ "$DATABASE_URL" == *"-pooler"* ]] && [[ "$DATABASE_URL" == *"neon.tech"* ]]; then
  # Neon pooler pattern: ep-xxx-pooler.region.aws.neon.tech
  # Transform to direct endpoint by removing "-pooler" from hostname
  # Use precise sed to only match hostname portion (avoid password issues)
  MIGRATION_URL=$(echo "$DATABASE_URL" | sed 's/\(@[^/]*\)-pooler\./\1./')
  echo "Using Neon direct endpoint (bypassing connection pooler)"
else
  # Local/QA Docker PostgreSQL - use as-is
  MIGRATION_URL="$DATABASE_URL"
  echo "Using DATABASE_URL directly (no pooler transformation needed)"
fi

# Neon-specific connection enhancements
if [[ "$MIGRATION_URL" == *"neon.tech"* ]]; then
  # Ensure sslmode=require for Neon connections (defensive check)
  if [[ "$MIGRATION_URL" != *"sslmode="* ]]; then
    if [[ "$MIGRATION_URL" == *"?"* ]]; then
      MIGRATION_URL="${MIGRATION_URL}&sslmode=require"
    else
      MIGRATION_URL="${MIGRATION_URL}?sslmode=require"
    fi
    echo "Added sslmode=require for Neon connection"
  fi

  # Add connect_timeout for serverless cold starts
  if [[ "$MIGRATION_URL" != *"connect_timeout="* ]]; then
    MIGRATION_URL="${MIGRATION_URL}&connect_timeout=15"
    echo "Added connect_timeout=15 for Neon serverless"
  fi

  # Warm up Neon compute before running migrations (handle autosuspend)
  echo "Warming up Neon database connection..."
  WARM_UP_ATTEMPTS=3
  for i in $(seq 1 $WARM_UP_ATTEMPTS); do
    if atlas migrate status \
        --config file:///app/atlas.hcl \
        --env production \
        --dir "file:///app/migrations?format=golang-migrate" \
        --url "$MIGRATION_URL" > /dev/null 2>&1; then
      echo "Database connection ready"
      break
    fi
    if [ $i -lt $WARM_UP_ATTEMPTS ]; then
      echo "Waiting for database to wake up... (attempt $i/$WARM_UP_ATTEMPTS)"
      sleep 3
    fi
  done
fi

# Run migrations using Atlas with direct connection
# Use --revisions-schema public to store revisions table in public schema
# This prevents Atlas from creating a separate atlas_schema_revisions schema
echo "Applying database migrations..."
MAX_ATTEMPTS=5
for i in $(seq 1 $MAX_ATTEMPTS); do
  echo "Migration attempt $i/$MAX_ATTEMPTS..."

  # Apply all pending migrations (Atlas handles fresh databases automatically)
  # Use atlas.hcl config for golang-migrate format support (.up.sql/.down.sql)
  # Explicit --dir with format parameter for consistency with CI/CD lint command
  atlas migrate apply \
    --config file:///app/atlas.hcl \
    --env production \
    --dir "file:///app/migrations?format=golang-migrate" \
    --url "$MIGRATION_URL" \
    --revisions-schema public && {
    echo "Migrations completed successfully"
    break
  } || {
    if [ $i -eq $MAX_ATTEMPTS ]; then
      echo "Migration failed after $MAX_ATTEMPTS attempts"
      echo "Run 'atlas migrate status' for details"
      exit 1
    else
      # Longer wait for first retry on Neon (cold start scenario)
      if [ $i -eq 1 ] && [[ "$MIGRATION_URL" == *"neon.tech"* ]]; then
        echo "First attempt failed (possible cold start), waiting 15s before retry..."
        sleep 15
      else
        echo "Migration attempt $i failed, retrying in 10s..."
        sleep 10
      fi
    fi
  }
done

echo "Starting server..."
exec /app/server
