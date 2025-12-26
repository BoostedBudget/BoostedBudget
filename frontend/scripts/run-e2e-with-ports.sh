#!/bin/bash
# Run E2E tests with dynamic port allocation for CI environments with concurrent runners
# This script finds available ports and exports them before running Playwright tests

set -e

# Find available ports
echo "Finding available ports for E2E tests..."
PORTS_OUTPUT=$(bun scripts/find-available-ports.js)

# Export the environment variables
export $(echo "$PORTS_OUTPUT" | grep -E "^E2E_")

echo "Using ports:"
echo "  Frontend: $E2E_FRONTEND_PORT"
echo "  Backend: $E2E_BACKEND_PORT"

# Run Playwright tests directly (not via npm to avoid circular dependency)
# Scenario tests are excluded via testIgnore in playwright.config.ts (not E2E_SCENARIOS_MODE)
bunx playwright test "$@"
