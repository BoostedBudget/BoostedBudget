#!/bin/bash
# Run E2E tests with dynamic port allocation and temporary PostgreSQL database
# Issue #582: Enables full database integration testing in E2E tests
#
# This script:
#   1. Finds available ports for frontend, backend, and PostgreSQL
#   2. Starts a temporary PostgreSQL container (if Docker available)
#   3. Runs database migrations
#   4. Always rebuilds backend binary (prevents stale-binary/schema mismatches)
#   5. Runs Playwright tests
#   6. Cleans up PostgreSQL and any managed Redis containers on exit (including startup failures and
#      INT/TERM/HUP; cleanup failure turns an otherwise-green run red)
#
# Environment variables:
#   E2E_SKIP_POSTGRES=true  - Skip PostgreSQL setup (runs without database)

set -Eeuo pipefail

# Get script directory for relative paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="${SCRIPT_DIR}/../../../backend"

# Source postgres helper functions
source "${SCRIPT_DIR}/e2e-postgres.sh"

# Redis is part of a full local E2E run now: cache-freshness and distributed-rate-limit specs must
# not silently run against the no-op/memory fallbacks. Set E2E_START_REDIS=false only when a caller
# deliberately wants the historical fallback mode. Any explicitly supplied URL wins, so CI and
# shared environments can provision their own instances. The cache and limiter are always separate
# containers.
E2E_REDIS_IMAGE="${E2E_REDIS_IMAGE:-redis:7-alpine}"
E2E_CACHE_REDIS_CONTAINER=""
E2E_RATELIMIT_REDIS_CONTAINER=""
E2E_CLEANUP_DONE=0

_generate_redis_container_name() {
  local role="$1"
  local run_id="${GITHUB_RUN_ID:-}"
  if [ -n "$run_id" ]; then
    echo "e2e-${role}-redis-${run_id}-${GITHUB_RUN_ATTEMPT:-1}"
  else
    echo "e2e-${role}-redis-$$-$(date +%s)"
  fi
}

start_redis_instance() {
  local role="$1"
  local policy="$2"
  local container
  local port

  container=$(_generate_redis_container_name "$role")
  # Register ownership before docker run. Docker can create the named container and still return
  # non-zero (for example, after a daemon-side startup failure); the EXIT trap must be able to
  # remove that exact name in every partial-start path.
  if [ "$role" = "cache" ]; then
    E2E_CACHE_REDIS_CONTAINER="$container"
  else
    E2E_RATELIMIT_REDIS_CONTAINER="$container"
  fi
  echo "🔴 Starting Redis ${role} container: ${container} (${policy})..."
  docker rm -f "$container" >/dev/null 2>&1 || true

  if ! docker run -d \
    --name "$container" \
    -p 127.0.0.1::6379 \
    --memory=256m \
    "$E2E_REDIS_IMAGE" \
    redis-server --maxmemory-policy "$policy" >/dev/null; then
    echo "❌ Failed to start Redis ${role} container: ${container}"
    return 1
  fi

  port="$(docker port "$container" 6379/tcp 2>/dev/null | sed -n 's/.*://p' | head -1 || true)"
  if [[ ! "$port" =~ ^[0-9]+$ ]]; then
    echo "❌ Could not determine the dynamically assigned port for Redis ${role} (${container})"
    return 1
  fi
  for _ in $(seq 1 30); do
    if docker exec "$container" redis-cli ping 2>/dev/null | grep -q PONG; then
      echo "   ✅ Redis ${role} ready on port ${port}"
      if [ "$role" = "cache" ]; then
        export CACHE_REDIS_URL="redis://127.0.0.1:${port}/0"
        export CACHE_ENABLED="${CACHE_ENABLED:-true}"
      else
        export RATE_LIMIT_REDIS_URL="redis://127.0.0.1:${port}/0"
      fi
      return 0
    fi
    sleep 1
  done

  echo "❌ Redis ${role} failed to become ready"
  docker logs "$container" 2>/dev/null || true
  return 1
}

stop_redis_instance() {
  local container="$1"
  [ -z "$container" ] && return 0
  local existing
  existing="$(docker ps -aq --filter "name=^${container}$" 2>/dev/null || true)"
  [ -z "$existing" ] && return 0
  echo "🛑 Stopping Redis container: ${container}"
  if ! docker rm -f "$container" >/dev/null 2>&1; then
    echo "⚠️  Docker could not remove Redis container ${container}; retrying once"
    sleep 1
    if ! docker rm -f "$container" >/dev/null 2>&1; then
      echo "❌ Redis container cleanup failed: ${container}"
      return 1
    fi
  fi

  if docker ps -aq --filter "name=^${container}$" 2>/dev/null | grep -q .; then
    echo "❌ Redis container still exists after cleanup: ${container}"
    return 1
  fi
}

# Background health monitor — logs if PostgreSQL dies mid-run so failures have clear evidence
monitor_postgres() {
  local container_name
  container_name=$(docker ps --filter "name=e2e-postgres-" --format "{{.Names}}" 2>/dev/null | head -1)
  [ -z "$container_name" ] && return

  while true; do
    sleep 30
    local status
    status=$(docker inspect --format='{{.State.Status}}' "$container_name" 2>/dev/null || echo "missing")
    if [ "$status" != "running" ]; then
      echo ""
      echo "🚨 PostgreSQL container '$container_name' is $status! Remaining tests will likely fail."
      docker logs --tail 20 "$container_name" 2>/dev/null || true
      break
    fi
  done
}

# Cleanup function - called on exit (success or failure)
cleanup() {
  local test_status=$?
  local cleanup_status=0

  # EXIT can be reached through an explicit failure, a signal trap, or normal completion. Make
  # cleanup idempotent so a signal followed by EXIT cannot race or double-remove resources.
  if [ "$E2E_CLEANUP_DONE" -eq 1 ]; then
    return "$test_status"
  fi
  E2E_CLEANUP_DONE=1
  trap - EXIT INT TERM HUP

  kill "${MONITOR_PID:-}" 2>/dev/null || true
  wait "${MONITOR_PID:-}" 2>/dev/null || true
  echo ""
  echo "🧹 Cleaning up..."
  stop_redis_instance "$E2E_CACHE_REDIS_CONTAINER" || cleanup_status=1
  stop_redis_instance "$E2E_RATELIMIT_REDIS_CONTAINER" || cleanup_status=1
  stop_postgres || cleanup_status=1

  if [ "$cleanup_status" -ne 0 ]; then
    echo "❌ One or more E2E resources could not be de-provisioned."
    # Never hide the original test/startup failure. A clean test run becomes a failure if cleanup
    # itself failed, so CI cannot report green while leaving a container behind.
    [ "$test_status" -eq 0 ] && test_status=1
  else
    echo "✅ E2E resources de-provisioned"
  fi

  return "$test_status"
}

# Set up trap for cleanup on exit
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

echo "🔍 Finding available ports for E2E tests..."
PORTS_OUTPUT=$(node "${SCRIPT_DIR}/find-available-ports.js")

# Export the environment variables
export $(echo "$PORTS_OUTPUT" | grep -E "^E2E_")

# The backend's internal metrics listener binds a hardcoded port (default 9091) unless
# INTERNAL_METRICS_PORT overrides it, and a failed bind is fatal (Issue #1514). Route the
# dynamically-allocated E2E_METRICS_PORT into it BEFORE the backend starts, so parallel E2E
# runs / CI runners on this host never collide on 9091.
export INTERNAL_METRICS_PORT="$E2E_METRICS_PORT"

echo ""
echo "📍 Using ports: Frontend=$E2E_FRONTEND_PORT Backend=$E2E_BACKEND_PORT Postgres=$E2E_POSTGRES_PORT Prod=$E2E_PROD_PORT Metrics=$E2E_METRICS_PORT"
echo ""

# Pre-flight port check — find-available-ports.js only proves a port was free at scan
# time; a process can grab it in the gap before Playwright's webServer binds (e.g. an
# orphaned `vite preview --strictPort` from a killed local run whose grandchild survived
# under PPID 1). --strictPort refuses to fall back, so a squatted port makes Playwright
# wait out the full webServer timeout (120-180s) with no indication *why* — it just looks
# like a slow build. Fail fast here instead, naming the port and the PID holding it.
check_port_free() {
  local port="$1"
  local label="$2"
  local pid
  # lsof returns 1 when no process is listening; with pipefail enabled that must be an expected
  # empty result, not a fatal startup error.
  pid="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -1 || true)"
  if [ -n "$pid" ]; then
    echo ""
    echo "❌ Port $port ($label) is already in use by PID $pid:"
    ps -p "$pid" -o pid,ppid,etime,command 2>/dev/null || true
    echo ""
    echo "   find-available-ports.js reported this port as free, but something has since"
    echo "   bound it — likely an orphaned process from a previous run. Kill PID $pid (or"
    echo "   its parent, if that PID is a short-lived child) and re-run."
    exit 1
  fi
}

check_port_free "$E2E_FRONTEND_PORT" "frontend"
check_port_free "$E2E_BACKEND_PORT" "backend"
check_port_free "$E2E_PROD_PORT" "prod"
check_port_free "$E2E_METRICS_PORT" "metrics"
echo "Building backend binary..."
(cd "$BACKEND_DIR" && go build -o bin/server ./cmd/server)
echo "   Backend built successfully"
echo ""

if [ "${E2E_SKIP_POSTGRES:-}" = "true" ]; then
  echo ""
  echo "⏭️  Skipping PostgreSQL (E2E_SKIP_POSTGRES=true)"
  echo "   Backend will run in degraded mode without database"
else
  # Try to start PostgreSQL
  if check_docker; then
    echo ""
    echo "🗄️  Setting up PostgreSQL database..."

    if start_postgres "$E2E_POSTGRES_PORT"; then
      if wait_for_postgres "$E2E_POSTGRES_PORT" 30; then
        # Build and export DATABASE_URL
        export DATABASE_URL=$(build_database_url "$E2E_POSTGRES_PORT")
        echo "   Database URL: postgres://test:***@localhost:$E2E_POSTGRES_PORT/test"

        # Run migrations
        if ! run_migrations "$DATABASE_URL"; then
          echo ""
          echo "❌ Database migrations failed"
          exit 1
        fi

        # Seed test data (if seed file exists and Clerk user ID available).
        #
        # #1569 task 20 (A1): "skipped or failed" used to be one warning covering both, and a seed
        # that aborted on a SQL error was therefore indistinguishable from a developer running
        # without CLERK_SECRET_KEY. The whole suite then ran against an empty database and the
        # resulting spec failures looked like application bugs. seed_e2e_data now returns 1 for a
        # real failure and 2 for an absent precondition; only 2 is survivable.
        # `|| SEED_STATUS=$?`, NOT a bare call followed by `SEED_STATUS=$?`. #1569 task 20,
        # review round 1: line 17 of this script is `set -Eeuo pipefail`, so a bare call that
        # returns non-zero kills the script AT THE CALL SITE — before `SEED_STATUS=$?` runs, and
        # therefore before either branch below. Every message here was unreachable for the one
        # case it exists to explain. Verified with a probe: bare call -> subshell exit 3 with no
        # diagnostic; `|| st=$?` -> status captured and the branch taken.
        SEED_STATUS=0
        seed_e2e_data "$DATABASE_URL" || SEED_STATUS=$?
        if [ "$SEED_STATUS" -eq 1 ]; then
          echo ""
          echo "❌ Test data seeding FAILED — refusing to run E2E against an unseeded database."
          echo "   Re-run the seed by hand to see the SQL error:"
          echo "     psql -v ON_ERROR_STOP=1 \"\$DATABASE_URL\" -f apps/web/scripts/seed-e2e-data.sql"
          exit 1
        elif [ "$SEED_STATUS" -eq 2 ]; then
          echo "   ⚠️  Test data seeding SKIPPED (no seed file or no Clerk user ID); authenticated specs will fail"
        fi

        echo ""
        echo "✅ Database ready for E2E tests!"

        # Start background monitor to log if postgres dies mid-run
        monitor_postgres &
        MONITOR_PID=$!
      else
        echo ""
        echo "❌ PostgreSQL failed to become ready"
        exit 1
      fi
    else
      echo ""
      echo "❌ Failed to start PostgreSQL container"
      exit 1
    fi
  else
    echo ""
    echo "   Continuing without database (Docker not available)"
  fi
fi

# A local cache/rate-limit Redis pair is the default. This is the local equivalent of the two Redis
# services provisioned by CI. Explicit CACHE_REDIS_URL/RATE_LIMIT_REDIS_URL values are respected
# and only missing instances are started. If either managed instance fails, the EXIT trap rolls back
# every managed Redis and PostgreSQL container from this run.
if [ "${E2E_START_REDIS:-true}" = "true" ]; then
  if { [ -z "${CACHE_REDIS_URL:-}" ] || [ -z "${RATE_LIMIT_REDIS_URL:-}" ]; } && ! check_docker; then
    echo "❌ E2E_START_REDIS=true requires a running Docker daemon"
    exit 1
  fi
  if [ -z "${CACHE_REDIS_URL:-}" ]; then
    start_redis_instance "cache" "allkeys-lru" || exit $?
  fi
  if [ -z "${RATE_LIMIT_REDIS_URL:-}" ]; then
    start_redis_instance "ratelimit" "noeviction" || exit $?
  fi
fi

echo ""
echo "🧪 Starting E2E tests..."
echo ""

# Run Playwright tests via bunx (not npx) to use Bun's module resolution.
export E2E_REVENUECAT_FIXTURE_MODE=simulated
unset REVENUECAT_PROJECT_ID REVENUECAT_ENVIRONMENT REVENUECAT_SECRET_API_KEY REVENUECAT_GRANT_API_KEY REVENUECAT_WEBHOOK_AUTHORIZATION REVENUECAT_WEBHOOK_SIGNING_SECRET
# npx uses Node.js realpath caching which creates dual @playwright/test instances
# in Bun's symlink-based node_modules/.bun/ layout.
# Scenario tests are excluded via testIgnore in playwright.config.ts (not E2E_SCENARIOS_MODE)
bunx playwright test "$@"
