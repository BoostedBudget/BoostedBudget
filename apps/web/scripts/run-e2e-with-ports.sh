#!/bin/bash
# Run E2E tests with dynamic port allocation and temporary PostgreSQL database
# Issue #582: Enables full database integration testing in E2E tests
#
# This script:
#   1. Finds available ports for frontend, backend, and PostgreSQL
#   2. Starts a temporary PostgreSQL container (if Docker available)
#   3. Runs database migrations
#   4. Builds backend if binary doesn't exist
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

# Cleanup function - called on exit (success or failure)
cleanup() {
  echo ""
  echo "🧹 Cleaning up..."
  stop_postgres
}

# Set up trap for cleanup on exit
trap cleanup EXIT

# =============================================================================
# Find Available Ports
# =============================================================================

echo "🔍 Finding available ports for E2E tests..."
PORTS_OUTPUT=$(node "${SCRIPT_DIR}/find-available-ports.js")

# Export the environment variables
export $(echo "$PORTS_OUTPUT" | grep -E "^E2E_")

echo ""
echo "📍 Using ports:"
echo "   Frontend: $E2E_FRONTEND_PORT"
echo "   Backend:  $E2E_BACKEND_PORT"
echo "   Postgres: $E2E_POSTGRES_PORT"

# =============================================================================
# Build Backend (if needed)
# =============================================================================

if [ ! -f "${BACKEND_DIR}/bin/server" ]; then
  echo ""
  echo "🔨 Backend binary not found, building..."
  (cd "$BACKEND_DIR" && go build -o bin/server ./cmd/server)
  echo "   ✅ Backend built successfully"
fi

# =============================================================================
# Setup PostgreSQL Database
# =============================================================================

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

# =============================================================================
# Run E2E Tests
# =============================================================================

echo ""
echo "🧪 Starting E2E tests..."
echo ""

# Run Playwright tests via bunx (not npx) to use Bun's module resolution.
# npx uses Node.js realpath caching which creates dual @playwright/test instances
# in Bun's symlink-based node_modules/.bun/ layout.
# Scenario tests are excluded via testIgnore in playwright.config.ts (not E2E_SCENARIOS_MODE)
bunx playwright test "$@"
