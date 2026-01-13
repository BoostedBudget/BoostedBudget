#!/bin/bash
# Run isolated tests that need centralized mocks but are polluted in full suite
# Issue #575: These tests pass individually but fail when run after DashboardPage.test.tsx
#
# Pollution cause: DashboardPage.test.tsx mocks DashboardGrid and WidgetCatalog components,
# which persists globally due to Bun's mock.module() behavior.
#
# These tests need centralized mocks (registry, dashboard-context, etc.) so they
# can't use bunfig.real.toml. They run with the normal bunfig.toml but separately.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"
cd "$FRONTEND_DIR"

# Parse arguments
COVERAGE=false
for arg in "$@"; do
    case $arg in
        --coverage)
            COVERAGE=true
            shift
            ;;
    esac
done

echo "Running isolated tests (polluted by DashboardPage.test.tsx in full suite)..."

# Test files that are polluted in the full suite
# These need centralized mocks but run separately due to mock pollution
TEST_FILES=(
    "src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "src/features/dashboard/widgets/WidgetCatalog.test.tsx"
    # RulesPage polluted by RuleBuilder (sets empty rules array)
    "src/features/rules/components/RulesPage.test.tsx"
)

# Build test command
if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-isolated/"
    TEST_CMD="bun test --coverage --coverage-reporter=lcov --coverage-dir=coverage-isolated ${TEST_FILES[*]}"
else
    TEST_CMD="bun test ${TEST_FILES[*]}"
fi

# Run the tests
$TEST_CMD
