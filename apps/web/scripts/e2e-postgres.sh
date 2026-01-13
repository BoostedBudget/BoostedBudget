#!/bin/bash
# e2e-postgres.sh - PostgreSQL management for E2E tests
# Issue #582: Temporary PostgreSQL for E2E tests
#
# This script provides modular functions for managing a temporary PostgreSQL
# container during E2E tests. Source this file to use its functions.
#
# Usage:
#   source e2e-postgres.sh
#   check_docker && start_postgres $PORT && wait_for_postgres $PORT
#   export DATABASE_URL=$(build_database_url $PORT)
#   run_migrations "$DATABASE_URL"
#   # ... run tests ...
#   stop_postgres  # Called automatically via trap in run-e2e-with-ports.sh

# =============================================================================
# Configuration
# =============================================================================

E2E_POSTGRES_USER="${E2E_POSTGRES_USER:-test}"
E2E_POSTGRES_PASSWORD="${E2E_POSTGRES_PASSWORD:-test}"
E2E_POSTGRES_DB="${E2E_POSTGRES_DB:-test}"
E2E_POSTGRES_IMAGE="${E2E_POSTGRES_IMAGE:-postgres:15}"

# Container name (set by start_postgres)
E2E_POSTGRES_CONTAINER=""

# Script directory (captured at source time for reliable path resolution)
# This is needed because BASH_SOURCE behaves differently when called from functions
# Note: Using E2E_ prefix (no leading underscore) to avoid conflicts with $_ special variable
E2E_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_BACKEND_DIR="${E2E_SCRIPT_DIR}/../../../backend"

# =============================================================================
# Helper Functions
# =============================================================================

# Generate unique container name for parallel execution support
_generate_container_name() {
  local run_id="${GITHUB_RUN_ID:-}"
  if [ -n "$run_id" ]; then
    # CI: Use GitHub run ID for unique naming
    echo "e2e-postgres-${run_id}"
  else
    # Local: Use PID + timestamp for unique naming
    echo "e2e-postgres-$$-$(date +%s)"
  fi
}

# Clean up any orphaned e2e-postgres containers from previous failed runs
# This helps prevent port conflicts and resource leaks
cleanup_orphan_containers() {
  local containers
  containers=$(docker ps -a --filter "name=e2e-postgres-" --format "{{.Names}}" 2>/dev/null)

  if [ -n "$containers" ]; then
    echo "🧹 Cleaning up orphaned e2e-postgres containers..."
    for container in $containers; do
      echo "   Removing: $container"
      docker stop "$container" 2>/dev/null || true
      docker rm -f "$container" 2>/dev/null || true
    done
    echo "   ✅ Orphan cleanup complete"
  fi
}

# Check if a port is already in use
_port_in_use() {
  local port="${1:?Error: Port required}"

  # Check if anything is listening on this port
  if lsof -i ":${port}" -sTCP:LISTEN &>/dev/null; then
    return 0  # Port is in use
  fi
  return 1  # Port is free
}

# =============================================================================
# Public Functions
# =============================================================================

# Check if Docker is available and running
# Returns 0 if Docker is available, 1 otherwise
check_docker() {
  if ! command -v docker &> /dev/null; then
    echo "⚠️  Docker not found. Running without database."
    echo "   Install Docker to enable full E2E testing with database."
    return 1
  fi

  if ! docker info &> /dev/null; then
    echo "⚠️  Docker daemon not running. Running without database."
    echo "   Start Docker to enable full E2E testing with database."
    return 1
  fi

  return 0
}

# Start PostgreSQL container on specified port
# Arguments:
#   $1 - Port to expose PostgreSQL on (required)
# Returns 0 on success, 1 on failure
start_postgres() {
  local port="${1:?Error: Port required for start_postgres}"

  # Clean up any orphaned containers first (prevents port conflicts)
  cleanup_orphan_containers

  # Check if port is still in use after cleanup (external process may be using it)
  if _port_in_use "$port"; then
    echo "❌ Port $port is still in use after cleanup"
    echo "   Check for other processes using this port:"
    echo "   lsof -i :$port"
    return 1
  fi

  E2E_POSTGRES_CONTAINER=$(_generate_container_name)

  echo "🐘 Starting PostgreSQL container: $E2E_POSTGRES_CONTAINER on port $port..."

  # Clean up any existing container with same name (from previous failed run)
  docker stop "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true
  docker rm "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true

  # Pull image with retry logic (network can be flaky)
  echo "   Pulling PostgreSQL image..."
  for i in 1 2 3; do
    docker pull "$E2E_POSTGRES_IMAGE" && break || {
      echo "   ⚠️  Pull attempt $i/3 failed"
      [ $i -lt 3 ] && sleep 5
    }
  done

  # Start container with explicit port mapping
  docker run -d \
    --name "$E2E_POSTGRES_CONTAINER" \
    -e POSTGRES_USER="$E2E_POSTGRES_USER" \
    -e POSTGRES_PASSWORD="$E2E_POSTGRES_PASSWORD" \
    -e POSTGRES_DB="$E2E_POSTGRES_DB" \
    -p "${port}:5432" \
    --health-cmd "pg_isready -U $E2E_POSTGRES_USER" \
    --health-interval 5s \
    --health-timeout 3s \
    --health-retries 10 \
    "$E2E_POSTGRES_IMAGE"

  if [ $? -ne 0 ]; then
    echo "❌ Failed to start PostgreSQL container"
    return 1
  fi

  echo "   ✅ PostgreSQL container started: $E2E_POSTGRES_CONTAINER"
  return 0
}

# Wait for PostgreSQL to be ready to accept connections
# Arguments:
#   $1 - Port (used for logging, actual check uses container name)
#   $2 - Max attempts (default: 30)
# Returns 0 when ready, 1 on timeout
wait_for_postgres() {
  local port="${1:?Error: Port required for wait_for_postgres}"
  local max_attempts="${2:-30}"

  echo "   Waiting for PostgreSQL to be ready..."

  for i in $(seq 1 $max_attempts); do
    if docker exec "$E2E_POSTGRES_CONTAINER" pg_isready -U "$E2E_POSTGRES_USER" > /dev/null 2>&1; then
      echo "   ✅ PostgreSQL is ready on port $port!"
      return 0
    fi

    if [ $i -eq $max_attempts ]; then
      echo "❌ Timeout waiting for PostgreSQL to start after ${max_attempts}s"
      echo "   Container logs:"
      docker logs "$E2E_POSTGRES_CONTAINER" 2>&1 | tail -20
      return 1
    fi

    # Show progress every 5 attempts
    if [ $((i % 5)) -eq 0 ]; then
      echo "   Waiting... ($i/$max_attempts)"
    fi
    sleep 1
  done
}

# Run database migrations using golang-migrate
# Arguments:
#   $1 - DATABASE_URL (required)
# Returns 0 on success, 1 on failure
run_migrations() {
  local database_url="${1:?Error: DATABASE_URL required for run_migrations}"

  echo "📦 Running database migrations..."

  # Check if migrate is available
  if ! command -v migrate &> /dev/null; then
    echo "❌ golang-migrate CLI not found."
    echo ""
    echo "   Install golang-migrate to run database migrations:"
    echo "   macOS:   brew install golang-migrate"
    echo "   Linux:   curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xvz"
    echo ""
    echo "   See: https://github.com/golang-migrate/migrate"
    return 1
  fi

  # Use backend directory captured at source time
  local backend_dir="${E2E_BACKEND_DIR}"

  # Verify backend directory exists
  if [ ! -d "$backend_dir" ]; then
    echo "❌ Backend directory not found: $backend_dir"
    echo "   (Script dir: ${E2E_SCRIPT_DIR})"
    return 1
  fi

  # Verify migrations directory exists
  if [ ! -d "${backend_dir}/migrations" ]; then
    echo "❌ Migrations directory not found: ${backend_dir}/migrations"
    return 1
  fi

  # Run migrations using golang-migrate
  migrate -database "$database_url" -path "${backend_dir}/migrations" up

  if [ $? -ne 0 ]; then
    echo "❌ Database migrations failed"
    return 1
  fi

  echo "   ✅ Migrations completed successfully!"
  return 0
}

# Stop and remove PostgreSQL container
# Safe to call multiple times (idempotent)
# Also cleans up any orphaned containers to prevent resource leaks
stop_postgres() {
  if [ -n "$E2E_POSTGRES_CONTAINER" ]; then
    echo "🛑 Stopping PostgreSQL container: $E2E_POSTGRES_CONTAINER"
    docker stop "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true
    docker rm -f "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true
    E2E_POSTGRES_CONTAINER=""
  fi

  # Also clean up any orphaned containers (from crashed runs, parallel executions, etc.)
  cleanup_orphan_containers
}

# Build DATABASE_URL from port
# Arguments:
#   $1 - Port (required)
# Outputs: PostgreSQL connection string
build_database_url() {
  local port="${1:?Error: Port required for build_database_url}"
  echo "postgres://${E2E_POSTGRES_USER}:${E2E_POSTGRES_PASSWORD}@localhost:${port}/${E2E_POSTGRES_DB}?sslmode=disable"
}

# Get Clerk user ID for the test user
# Uses E2E_CLERK_USER_ID if set, otherwise queries Clerk API
# Outputs: Clerk user ID (user_xxx) or empty string on failure
_get_clerk_user_id() {
  # If already set in environment, use that
  if [ -n "${E2E_CLERK_USER_ID:-}" ]; then
    echo "$E2E_CLERK_USER_ID"
    return 0
  fi

  # Try to get from Clerk API using the test email
  local test_email="${TEST_CLERK_EMAIL:-}"
  local secret_key="${CLERK_SECRET_KEY:-}"

  if [ -z "$test_email" ] || [ -z "$secret_key" ]; then
    return 1
  fi

  # Query Clerk API for users by email
  # Using curl with jq to parse response
  if ! command -v jq &> /dev/null; then
    echo "   ⚠️  jq not found, cannot query Clerk API" >&2
    return 1
  fi

  local response
  response=$(curl -s -X GET "https://api.clerk.dev/v1/users?email_address=${test_email}" \
    -H "Authorization: Bearer ${secret_key}" \
    -H "Content-Type: application/json" 2>/dev/null)

  if [ $? -ne 0 ]; then
    return 1
  fi

  # Extract user ID from first result
  local user_id
  user_id=$(echo "$response" | jq -r '.[0].id // empty' 2>/dev/null)

  if [ -n "$user_id" ] && [ "$user_id" != "null" ]; then
    echo "$user_id"
    return 0
  fi

  return 1
}

# Seed E2E test data into the database
# Arguments:
#   $1 - DATABASE_URL (required)
# Returns 0 on success, 1 on failure (non-fatal)
seed_e2e_data() {
  local database_url="${1:?Error: DATABASE_URL required for seed_e2e_data}"
  local seed_file="${E2E_SCRIPT_DIR}/seed-e2e-data.sql"

  # Check if seed file exists
  if [ ! -f "$seed_file" ]; then
    echo "   ⚠️  Seed file not found: $seed_file"
    return 1
  fi

  echo "🌱 Seeding E2E test data..."

  # Get Clerk user ID
  local clerk_user_id
  clerk_user_id=$(_get_clerk_user_id)

  if [ -z "$clerk_user_id" ]; then
    echo "   ⚠️  Could not determine Clerk user ID"
    echo "   Set E2E_CLERK_USER_ID or ensure TEST_CLERK_EMAIL and CLERK_SECRET_KEY are set"
    return 1
  fi

  echo "   Using Clerk user ID: $clerk_user_id"

  # Create temp file with replaced placeholder
  local temp_file
  temp_file=$(mktemp)
  sed "s/__E2E_CLERK_USER_ID__/${clerk_user_id}/g" "$seed_file" > "$temp_file"

  # Run SQL via psql inside the Docker container
  # Copy the temp file to the container and execute it
  docker cp "$temp_file" "${E2E_POSTGRES_CONTAINER}:/tmp/seed.sql"

  docker exec "$E2E_POSTGRES_CONTAINER" \
    psql -U "$E2E_POSTGRES_USER" -d "$E2E_POSTGRES_DB" -f /tmp/seed.sql \
    > /dev/null 2>&1

  local exit_code=$?

  # Clean up temp file
  rm -f "$temp_file"

  if [ $exit_code -ne 0 ]; then
    echo "   ⚠️  Seed script failed (exit code: $exit_code)"
    return 1
  fi

  echo "   ✅ Test data seeded successfully!"
  return 0
}
