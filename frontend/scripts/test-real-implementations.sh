#!/bin/bash
# Run tests that need REAL implementations (not centralized mocks)
# Issue #575: registry.test.ts and DashboardContext.test.tsx
#
# These tests verify the REAL behavior of modules that are mocked
# in centralized preloads. They run with bunfig.real.toml which
# excludes mock preloads.

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

echo "Running real-implementation tests..."
echo "Temporarily swapping bunfig.toml for bunfig.real.toml"

# Backup current bunfig.toml
mv bunfig.toml bunfig.toml.bak

# Use the real-implementation config
cp bunfig.real.toml bunfig.toml

# Build test command
if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-real/"
    TEST_CMD="bun test --coverage --coverage-reporter=lcov --coverage-dir=coverage-real src/features/dashboard/widgets/registry.test.ts src/features/dashboard/context/DashboardContext.test.tsx"
else
    TEST_CMD="bun test src/features/dashboard/widgets/registry.test.ts src/features/dashboard/context/DashboardContext.test.tsx"
fi

# Run the tests
set +e
$TEST_CMD
TEST_EXIT_CODE=$?
set -e

# Restore original bunfig.toml
mv bunfig.toml.bak bunfig.toml

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✓ Real-implementation tests passed!"
else
    echo "✗ Real-implementation tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE
