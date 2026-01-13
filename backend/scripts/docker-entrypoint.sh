#!/bin/bash
set -e

echo "Starting BoostedBudget Backend..."

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL environment variable is not set"
  exit 1
fi

echo "Database URL configured"

# Verify golang-migrate tool is available
if ! command -v migrate &> /dev/null; then
  echo "ERROR: golang-migrate tool not found in PATH"
  exit 1
fi

echo "Running database migrations with golang-migrate..."

# Construct direct PostgreSQL connection for migrations (bypass connection pooler)
# golang-migrate is incompatible with connection poolers (PgBouncer/Neon pooler) because
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
  # Ensure sslmode=require for Neon connections
  if [[ "$MIGRATION_URL" != *"sslmode="* ]]; then
    MIGRATION_URL="${MIGRATION_URL}$(if [[ "$MIGRATION_URL" == *"?"* ]]; then echo "&"; else echo "?"; fi)sslmode=require"
    echo "Added sslmode=require for Neon connection"
  fi

  # Add connect_timeout for serverless cold starts
  if [[ "$MIGRATION_URL" != *"connect_timeout="* ]]; then
    MIGRATION_URL="${MIGRATION_URL}&connect_timeout=15"
    echo "Added connect_timeout=15 for Neon serverless"
  fi
fi

# Auto-heal dirty migration 40 (from failed #674-678 tier restructure)
# This is idempotent and can be safely run multiple times
check_output=$(migrate -database "$MIGRATION_URL" -path /app/migrations version 2>&1 || true)
if echo "$check_output" | grep -q "40 (dirty)"; then
  echo "Detected dirty migration 40, applying auto-fix..."

  # Execute idempotent SQL to complete migration 40
  psql "$MIGRATION_URL" <<'EOSQL'
DO $$ BEGIN
  -- Rename column if still using old name
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'users' AND column_name = 'stripe_customer_id') THEN
    ALTER TABLE users RENAME COLUMN stripe_customer_id TO paddle_customer_id;
  END IF;
END $$;

-- Drop old constraints
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_tier;
ALTER TABLE users DROP CONSTRAINT IF EXISTS valid_subscription_status;

-- Migrate tier data (idempotent via WHERE clause)
UPDATE users SET subscription_tier = 'premium', subscription_status = 'trial'
  WHERE subscription_tier = 'free';
UPDATE users SET subscription_tier = 'premium'
  WHERE subscription_tier = 'premium_lite';
UPDATE users SET subscription_tier = 'household_5'
  WHERE subscription_tier = 'premium_plus';

-- Add new constraints
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'valid_subscription_tier') THEN
    ALTER TABLE users ADD CONSTRAINT valid_subscription_tier
      CHECK (subscription_tier IN ('premium', 'household_2', 'household_3', 'household_4', 'household_5'));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'valid_subscription_status') THEN
    ALTER TABLE users ADD CONSTRAINT valid_subscription_status
      CHECK (subscription_status IN ('active', 'inactive', 'cancelled', 'past_due', 'trial'));
  END IF;
END $$;

-- Clear dirty flag
UPDATE schema_migrations SET dirty = false WHERE version = 40;
EOSQL

  echo "Migration 40 auto-fix completed successfully"
fi

# Run migrations with retry logic
echo "Applying database migrations..."
MAX_ATTEMPTS=5
for i in $(seq 1 $MAX_ATTEMPTS); do
  echo "Migration attempt $i/$MAX_ATTEMPTS..."

  # Apply all pending migrations using golang-migrate
  migrate -database "$MIGRATION_URL" -path /app/migrations up && {
    echo "Migrations completed successfully"
    break
  } || {
    if [ $i -eq $MAX_ATTEMPTS ]; then
      echo "Migration failed after $MAX_ATTEMPTS attempts"
      echo "Run 'migrate version' for details"
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
