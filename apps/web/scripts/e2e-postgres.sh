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
# postgres:17 to match CI's own containers, docker-compose, and squawk's --pg-version=17.
#
# This defaulted to postgres:15 while everything else moved to 17, and because this harness runs the
# WHOLE E2E suite — applying all 129 migrations via run_migrations() below — the suite's results were
# being produced against a different major version than the application is developed and linted
# against. It was the last and least visible place that drift survived: a sibling commit bumped
# docker-compose from 15 to 17 and claimed to close the local-vs-CI gap, but never touched this file,
# so the gap simply moved here. Found by a round-3 audit.
E2E_POSTGRES_IMAGE="${E2E_POSTGRES_IMAGE:-postgres:17}"

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

# Clean up e2e-postgres containers from previous failed runs.
# In CI: only cleans up containers from the CURRENT run to avoid killing sibling
# runs' databases on shared Mac Mini runners (multiple PRs can run concurrently).
# Locally: cleans all e2e containers (safe — no concurrent runs).
cleanup_orphan_containers() {
  local current_run_id="${GITHUB_RUN_ID:-}"

  if [ -n "$current_run_id" ]; then
    # CI: scope to current run only — never kill another run's database
    local container="e2e-postgres-${current_run_id}"
    if docker ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^${container}$"; then
      echo "🧹 Cleaning up container from previous attempt: $container"
      docker stop "$container" 2>/dev/null || true
      docker rm -f "$container" 2>/dev/null || true
    fi
  else
    # Local: clean all e2e containers (no concurrent local runs)
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

  # Start container with explicit port mapping and resource limits.
  # --memory=512m: cap OOM risk on shared Mac Mini runners with concurrent CI jobs
  # --shm-size=256m: PostgreSQL uses /dev/shm for IPC; Docker default 64MB can cause crashes
  docker run -d \
    --name "$E2E_POSTGRES_CONTAINER" \
    -e POSTGRES_USER="$E2E_POSTGRES_USER" \
    -e POSTGRES_PASSWORD="$E2E_POSTGRES_PASSWORD" \
    -e POSTGRES_DB="$E2E_POSTGRES_DB" \
    -p "${port}:5432" \
    --memory=512m \
    --shm-size=256m \
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

# Wait for PostgreSQL to be ready to accept connections.
#
# Uses an actual SQL query (SELECT 1) rather than pg_isready alone, because
# Docker's postgres entrypoint has a two-phase startup: an init phase (temp
# postgres for DB creation) that accepts pg_isready but then shuts down, followed
# by the real production startup. pg_isready can pass during the init phase,
# causing "connection reset by peer" when the migration tool connects during
# the shutdown-restart window. SELECT 1 against the target database only
# succeeds once the production postgres is fully ready.
#
# Arguments:
#   $1 - Port (used for logging, actual check uses container name)
#   $2 - Max attempts (default: 30)
# Returns 0 when ready, 1 on timeout
wait_for_postgres() {
  local port="${1:?Error: Port required for wait_for_postgres}"
  local max_attempts="${2:-30}"

  echo "   Waiting for PostgreSQL to be ready..."

  for i in $(seq 1 $max_attempts); do
    # Use an actual query instead of pg_isready to avoid the init-phase race condition
    if docker exec "$E2E_POSTGRES_CONTAINER" \
      psql -U "$E2E_POSTGRES_USER" -d "$E2E_POSTGRES_DB" -c "SELECT 1" > /dev/null 2>&1; then
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
  local cleanup_status=0

  if [ -n "$E2E_POSTGRES_CONTAINER" ]; then
    local existing
    existing="$(docker ps -aq --filter "name=^${E2E_POSTGRES_CONTAINER}$" 2>/dev/null || true)"
    if [ -z "$existing" ]; then
      E2E_POSTGRES_CONTAINER=""
      return 0
    fi
    echo "🛑 Stopping PostgreSQL container: $E2E_POSTGRES_CONTAINER"
    if ! docker rm -f "$E2E_POSTGRES_CONTAINER" 2>/dev/null; then
      echo "⚠️  Docker could not remove PostgreSQL container ${E2E_POSTGRES_CONTAINER}; retrying once"
      sleep 1
      if ! docker rm -f "$E2E_POSTGRES_CONTAINER" 2>/dev/null; then
        echo "❌ PostgreSQL container cleanup failed: ${E2E_POSTGRES_CONTAINER}"
        cleanup_status=1
      fi
    fi

    if docker ps -aq --filter "name=^${E2E_POSTGRES_CONTAINER}$" 2>/dev/null | grep -q .; then
      echo "❌ PostgreSQL container still exists after cleanup: ${E2E_POSTGRES_CONTAINER}"
      cleanup_status=1
    fi
    E2E_POSTGRES_CONTAINER=""
  fi

  # Orphan cleanup remains a startup concern. Keeping it out of the EXIT path means a failed run
  # only de-provisions the container it owns; it cannot broad-delete a concurrent local run's DB.
  return "$cleanup_status"
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

  # URL-encode the email address before passing as query parameter
  # The + character in emails (e.g. e2e+test@domain.com) must be encoded as %2B
  local encoded_email
  encoded_email=$(python3 -c "import urllib.parse; print(urllib.parse.quote('${test_email}', safe='@'))" 2>/dev/null)
  if [ -z "$encoded_email" ]; then
    encoded_email="$test_email"
  fi

  local response
  response=$(curl -s -X GET "https://api.clerk.dev/v1/users?email_address=${encoded_email}" \
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
# Returns:
#   0 - seeded
#   1 - the seed FAILED (SQL error). Fatal: the suite would run against an empty database.
#   2 - seeding was SKIPPED because a precondition is absent (no seed file, no Clerk user id).
#       Not fatal — a developer without CLERK_SECRET_KEY can still run unauthenticated specs.
#
# #1569 task 20 (A1): the 1-vs-2 split is load-bearing. This function used to return 1 for both,
# and the caller downgraded 1 to a warning so that the legitimate skip stayed quiet -- which meant
# a real SQL failure was quiet too. Pinned by scripts/__tests__/check-e2e-seed-fails-loudly.test.sh.
seed_e2e_data() {
  local database_url="${1:?Error: DATABASE_URL required for seed_e2e_data}"
  local seed_file="${E2E_SCRIPT_DIR}/seed-e2e-data.sql"

  # Check if seed file exists
  if [ ! -f "$seed_file" ]; then
    echo "   ⚠️  Seed file not found: $seed_file"
    return 2
  fi

  echo "🌱 Seeding E2E test data..."

  # Get Clerk user ID.
  #
  # `|| true` is required, not defensive. #1569 task 20, review round 1: this function is sourced
  # into run-e2e-with-ports.sh, which runs under `set -Eeuo pipefail`, and a plain assignment from
  # a FAILING command substitution aborts the shell — verified with a standalone probe, in which
  # the line after the assignment never executed. _get_clerk_user_id returns 1 whenever no id can
  # be determined, so without this the `return 2` skip below was unreachable in production and a
  # developer with no CLERK_SECRET_KEY got a bare abort instead of the SKIPPED warning.
  local clerk_user_id
  clerk_user_id=$(_get_clerk_user_id) || true

  if [ -z "$clerk_user_id" ]; then
    echo "   ⚠️  Could not determine Clerk user ID"
    echo "   Set E2E_CLERK_USER_ID or ensure TEST_CLERK_EMAIL and CLERK_SECRET_KEY are set"
    return 2
  fi

  echo "   Using Clerk user ID: $clerk_user_id"

  # Create temp file with replaced placeholder
  local temp_file
  temp_file=$(mktemp)
  sed "s/__E2E_CLERK_USER_ID__/${clerk_user_id}/g" "$seed_file" > "$temp_file"

  # Run SQL via psql inside the Docker container
  # Copy the temp file to the container and execute it
  if ! docker cp "$temp_file" "${E2E_POSTGRES_CONTAINER}:/tmp/seed.sql"; then
    rm -f "$temp_file"
    echo "   ❌ Could not copy the seed script into ${E2E_POSTGRES_CONTAINER}"
    return 1
  fi

  # #1569 task 20 (A1). Three deliberate details, each of which was individually enough to hide a
  # totally failed seed:
  #
  #   -v ON_ERROR_STOP=1  -- WITHOUT it, psql -f reports ERROR and then exits 0. Every check below
  #                          would be reading a zero from a run that seeded nothing.
  #   no `|| true`        -- the old code ended `grep -E ... || true`, which pinned the pipeline's
  #                          status at 0. `local exit_code=$?` was then read AFTER it, so it
  #                          captured `|| true`'s zero and never psql's status at all.
  #   NO PIPELINE AT ALL  -- and this is the correction to the first version of this fix, which
  #                          piped through `tee` and read PIPESTATUS[0]. See below.
  #
  # ⚠ WHY THE PIPE IS GONE, AND WHY PIPESTATUS IS NOT USED. #1569 task 20, review round 1.
  #
  # This function is SOURCED INTO run-e2e-with-ports.sh, whose line 17 is `set -Eeuo pipefail` and
  # has been since long before this change. Under errexit + pipefail, `docker exec … | tee` KILLS
  # THE SHELL the instant psql exits non-zero. The PIPESTATUS read, the diagnostic block, the
  # 1-vs-2 return contract and the caller's re-run hint below are all AFTER that pipeline, so for
  # the actual defect class they never executed. Observed, not reasoned: with the original
  # ON CONFLICT defect reintroduced, the real harness exited 3, Playwright never started, and none
  # of the messages below appeared anywhere in the log.
  #
  # A redirect plus `|| exit_code=$?` is immune to both options and needs neither:
  #   * errexit does not fire for a command on the left of `||`, so control reaches the diagnostic;
  #   * there is no pipeline, so the status is psql's own and pipefail is irrelevant. The earlier
  #     PIPESTATUS spelling silently depended on the CALLER having pipefail set — without it the
  #     pipeline reports tee's 0 and the failure is invisible again.
  # The Issue #698 debugging view is preserved by cat-ing the log instead of tee-ing to it.
  local seed_log="${temp_file}.log"
  local exit_code=0
  docker exec "$E2E_POSTGRES_CONTAINER" \
    psql -v ON_ERROR_STOP=1 -U "$E2E_POSTGRES_USER" -d "$E2E_POSTGRES_DB" -f /tmp/seed.sql \
    > "$seed_log" 2>&1 || exit_code=$?
  cat "$seed_log"

  # Clean up temp file
  rm -f "$temp_file"

  if [ "$exit_code" -ne 0 ]; then
    # The output is NOT re-printed here: the `cat` above already showed it, immediately preceding
    # this line. An earlier version dumped it twice, which on a real failure meant ~20 duplicated
    # lines between the developer and the message that matters.
    echo "   ❌ Seed script FAILED (psql exit code: $exit_code) — see the psql output immediately above"
    rm -f "$seed_log"
    return 1
  fi

  # Belt and braces: ON_ERROR_STOP makes psql exit non-zero on an ERROR, but a seed that starts
  # RAISE-ing warnings without failing is still worth refusing to call a success.
  if grep -q "^ERROR:\|: ERROR:" "$seed_log" 2>/dev/null; then
    echo "   ❌ Seed script printed an ERROR despite exiting 0:"
    grep "^ERROR:\|: ERROR:" "$seed_log"
    rm -f "$seed_log"
    return 1
  fi

  rm -f "$seed_log"
  echo "   ✅ Test data seeded successfully!"
  return 0
}
