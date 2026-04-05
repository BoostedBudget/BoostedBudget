#!/bin/bash
# Run tests that need REAL implementations (not centralized mocks)
# Issue #575: Hook implementation tests and registry tests
#
# These tests verify the REAL behavior of modules that are mocked
# in centralized preloads. They run with bunfig.real.toml which
# excludes mock preloads.
#
# CRITICAL: Must run from OUTSIDE apps/web/ directory to avoid
# loading bunfig.toml from CWD. Bun's --config is ADDITIVE, not
# a replacement. Running from /tmp ensures only bunfig.real.toml
# is loaded. We pass preload files directly since paths in config
# are resolved from CWD.
#
# Test files included:
# - registry.test.ts - Widget registry real implementation
# - DashboardContext.test.tsx - Dashboard context real implementation
# - useDashboardData.test.tsx - Dashboard data hook
# - useDashboardLayout.test.tsx - Dashboard layout hook
# - useDashboardOnboarding.test.ts - Dashboard onboarding hook
# - useBudgetFilters.test.ts - Budget filters hook
# - useBudgets.test.ts - Budgets hook
# - useSavingsGoals.test.tsx - Savings goals hook
# - useBudgetDetail.test.tsx - Budget detail hook
# - useAvailableCategories.test.tsx - Available categories hook

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"

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

# Test files that need real implementations (absolute paths)
TEST_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/widgets/registry.test.ts"
    "$FRONTEND_DIR/src/features/dashboard/context/DashboardContext.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardData.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetFilters.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgets.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useSavingsGoals.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useAvailableCategories.test.tsx"
)

# Preload files for real implementation tests (absolute paths)
# Only DOM, testing-library, and minimal API-level mocks needed by each test
# NO wrapper-level mocks that would interfere with testing real hook implementations
PRELOAD_FILES=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    "$FRONTEND_DIR/src/test/mocks/savings-goals-api.ts"
)

# Build preload arguments
PRELOAD_ARGS=$(printf -- '--preload %s ' "${PRELOAD_FILES[@]}")

# CRITICAL: Run from /tmp to avoid loading bunfig.toml from frontend CWD
# Bun's --config is ADDITIVE, not replacement. By running from /tmp,
# there's no bunfig.toml to load. We pass preload files directly via CLI
# with absolute paths to ensure they're found.
cd /tmp

# Build test command
if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-real/"
    bun test $PRELOAD_ARGS \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-real" \
        "${TEST_FILES[@]}"
else
    bun test $PRELOAD_ARGS --timeout 30000 "${TEST_FILES[@]}"
fi

echo ""
echo "✓ Real-implementation tests completed!"
