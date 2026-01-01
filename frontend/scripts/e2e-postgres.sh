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
E2E_BACKEND_DIR="${E2E_SCRIPT_DIR}/../../backend"

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

# Run database migrations using Atlas
# Arguments:
#   $1 - DATABASE_URL (required)
# Returns 0 on success, 1 on failure
run_migrations() {
  local database_url="${1:?Error: DATABASE_URL required for run_migrations}"

  echo "📦 Running database migrations..."

  # Check if atlas is available
  if ! command -v atlas &> /dev/null; then
    echo "❌ Atlas CLI not found."
    echo ""
    echo "   Install Atlas to run database migrations:"
    echo "   macOS:   brew install ariga/tap/atlas"
    echo "   Linux:   curl -sSf https://atlasgo.sh | sh"
    echo "   Windows: https://atlasgo.io/getting-started#installation"
    echo ""
    echo "   See: https://atlasgo.io/getting-started"
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

  # Verify atlas.hcl exists
  if [ ! -f "${backend_dir}/atlas.hcl" ]; then
    echo "❌ Atlas config not found: ${backend_dir}/atlas.hcl"
    return 1
  fi

  # Verify migrations directory exists
  if [ ! -d "${backend_dir}/migrations" ]; then
    echo "❌ Migrations directory not found: ${backend_dir}/migrations"
    return 1
  fi

  # Run migrations
  atlas migrate apply \
    --config "file://${backend_dir}/atlas.hcl" \
    --env local \
    --url "$database_url" \
    --dir "file://${backend_dir}/migrations?format=golang-migrate" \
    --revisions-schema public

  if [ $? -ne 0 ]; then
    echo "❌ Database migrations failed"
    return 1
  fi

  echo "   ✅ Migrations completed successfully!"
  return 0
}

# Stop and remove PostgreSQL container
# Safe to call multiple times (idempotent)
stop_postgres() {
  if [ -n "$E2E_POSTGRES_CONTAINER" ]; then
    echo "🛑 Stopping PostgreSQL container: $E2E_POSTGRES_CONTAINER"
    docker stop "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true
    docker rm "$E2E_POSTGRES_CONTAINER" 2>/dev/null || true
    E2E_POSTGRES_CONTAINER=""
  fi
}

# Build DATABASE_URL from port
# Arguments:
#   $1 - Port (required)
# Outputs: PostgreSQL connection string
build_database_url() {
  local port="${1:?Error: Port required for build_database_url}"
  echo "postgres://${E2E_POSTGRES_USER}:${E2E_POSTGRES_PASSWORD}@localhost:${port}/${E2E_POSTGRES_DB}?sslmode=disable"
}
