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
#   6. Cleans up PostgreSQL container on exit
#
# Environment variables:
#   E2E_SKIP_POSTGRES=true  - Skip PostgreSQL setup (runs without database)

set -e

# Get script directory for relative paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="${SCRIPT_DIR}/../../../backend"

# Source postgres helper functions
source "${SCRIPT_DIR}/e2e-postgres.sh"

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
  kill "${MONITOR_PID:-}" 2>/dev/null || true
  echo ""
  echo "🧹 Cleaning up..."
  stop_postgres
}

# Set up trap for cleanup on exit
trap cleanup EXIT

echo "🔍 Finding available ports for E2E tests..."
PORTS_OUTPUT=$(node "${SCRIPT_DIR}/find-available-ports.js")

# Export the environment variables
export $(echo "$PORTS_OUTPUT" | grep -E "^E2E_")

echo ""
echo "📍 Using ports: Frontend=$E2E_FRONTEND_PORT Backend=$E2E_BACKEND_PORT Postgres=$E2E_POSTGRES_PORT Prod=$E2E_PROD_PORT"
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
  pid=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null | head -1)
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

        # Seed test data (if seed file exists and Clerk user ID available)
        if ! seed_e2e_data "$DATABASE_URL"; then
          echo "   ⚠️  Test data seeding skipped or failed (tests may have limited data)"
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

echo ""
echo "🧪 Starting E2E tests..."
echo ""

# Run Playwright tests via bunx (not npx) to use Bun's module resolution.
# npx uses Node.js realpath caching which creates dual @playwright/test instances
# in Bun's symlink-based node_modules/.bun/ layout.
# Scenario tests are excluded via testIgnore in playwright.config.ts (not E2E_SCENARIOS_MODE)
bunx playwright test "$@"
