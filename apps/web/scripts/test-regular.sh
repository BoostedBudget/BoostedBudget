#!/bin/bash
# Run regular tests (excluding real-implementation tests)
# Issue #575: Exclude tests that need real implementations
#
# This script runs all tests EXCEPT those that need real implementations.
# The real-implementation tests are run separately via test-real-implementations.sh

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

# Test files that need real implementations (will be excluded)
# These are run separately via test-real-implementations.sh
EXCLUDE_PATTERNS=(
    # Real implementation tests (no centralized mocks)
    "src/features/dashboard/widgets/registry.test.ts"
    "src/features/dashboard/context/DashboardContext.test.tsx"
    # Hook implementation tests (need API-level mocks, not wrapper mocks)
    "src/features/dashboard/hooks/useDashboardData.test.tsx"
    "src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    "src/features/budgets/hooks/useBudgetFilters.test.ts"
    "src/features/budgets/hooks/useBudgets.test.ts"
    "src/features/budgets/hooks/useSavingsGoals.test.tsx"
    "src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "src/features/budgets/hooks/useAvailableCategories.test.tsx"
    # Hook implementation tests (need API-level mocks, not wrapper mocks)
    "src/features/dashboard/hooks/useGettingStarted.test.ts"
    # Isolated tests polluted by other tests (run via test-isolated.sh)
    "src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "src/features/dashboard/widgets/WidgetCatalog.test.tsx"
    "src/features/rules/components/RulesPage.test.tsx"
    # AccountsPage uses local component mocks that can pollute AccountTable etc. (#839)
    "src/features/accounts/AccountsPage.test.tsx"
    # ToggleGroup component test needs real implementation but TimeframeSelector/DateRangePicker
    # tests mock @/components/ui/toggle-group globally, polluting it in the full suite (#953)
    "src/components/ui/toggle-group.test.tsx"
    # setSystemTime pollution: date mocks from other test files leak into these, causing
    # date mismatch failures in the full suite (#952)
    "src/features/dashboard/components/TimeframeSelector.test.tsx"
    "src/features/transactions/components/TransactionCard.test.tsx"
)

echo "Running regular tests (excluding real-implementation tests)..."

# Find all test files and exclude the real-implementation ones
ALL_TESTS=$(find src -name "*.test.ts" -o -name "*.test.tsx" | sort)

# Build the exclude pattern for grep -E (extended regex)
# Join patterns with | for alternation
EXCLUDE_GREP=$(printf '%s|' "${EXCLUDE_PATTERNS[@]}" | sed 's/|$//')

# Filter out excluded files using extended regex
REGULAR_TESTS=$(echo "$ALL_TESTS" | grep -vE "$EXCLUDE_GREP" || true)

if [ -z "$REGULAR_TESTS" ]; then
    echo "No regular tests found!"
    exit 1
fi

# Count tests
TOTAL_COUNT=$(echo "$ALL_TESTS" | wc -l | tr -d ' ')
REGULAR_COUNT=$(echo "$REGULAR_TESTS" | wc -l | tr -d ' ')
EXCLUDED_COUNT=$((TOTAL_COUNT - REGULAR_COUNT))

echo "Found $REGULAR_COUNT regular tests (excluding $EXCLUDED_COUNT real-implementation tests)"

# Build test command
if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage/"
    TEST_CMD="bun test --coverage --coverage-reporter=lcov --coverage-dir=coverage $REGULAR_TESTS"
else
    TEST_CMD="bun test $REGULAR_TESTS"
fi

# Run the tests
$TEST_CMD
