#!/bin/bash
# Run regular tests excluding those that need process isolation (test-conflict-files.sh).
# Issue #1323: Batch runner for centrally-mocked tests.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"
cd "$FRONTEND_DIR"

COVERAGE=false
for arg in "$@"; do
    case $arg in
        --coverage) COVERAGE=true; shift ;;
    esac
done

# Tests excluded (run via test-conflict-files.sh with process isolation)
EXCLUDE_PATTERNS=(
    # Conflict with centralized mocks
    "src/features/dashboard/widgets/registry.test.ts"
    "src/features/dashboard/context/DashboardContext.test.tsx"
    "src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "src/features/dashboard/widgets/WidgetCatalog.test.tsx"
    "src/components/ui/toggle-group.test.tsx"
    # Inline mock.module() pollution
    "src/features/accounts/AccountsPage.test.tsx"
    # Real-implementation hook tests
    "src/features/dashboard/hooks/useDashboardData.test.tsx"
    "src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    "src/features/dashboard/hooks/useGettingStarted.test.ts"
    "src/features/budgets/hooks/useBudgetFilters.test.ts"
    "src/features/budgets/hooks/useBudgets.test.ts"
    "src/features/budgets/hooks/useSavingsGoals.test.tsx"
    "src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "src/features/budgets/hooks/useAvailableCategories.test.tsx"
    "src/features/transactions/hooks/useInfiniteTransactions.test.tsx"
    "src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "src/features/budgets/hooks/usePreviewCategories.test.tsx"
)

echo "Running tests with centralized mocks (excluding process-isolated tests)..."

# Find and filter tests
ALL_TESTS=$(find src -name "*.test.ts" -o -name "*.test.tsx" | sort)
EXCLUDE_GREP=$(printf '%s|' "${EXCLUDE_PATTERNS[@]}" | sed 's/|$//')
REGULAR_TESTS=$(echo "$ALL_TESTS" | grep -vE "$EXCLUDE_GREP" || true)

if [ -z "$REGULAR_TESTS" ]; then
    echo "No regular tests found!"
    exit 1
fi

# Report counts
TOTAL_COUNT=$(echo "$ALL_TESTS" | wc -l | tr -d ' ')
REGULAR_COUNT=$(echo "$REGULAR_TESTS" | wc -l | tr -d ' ')
echo "Testing $REGULAR_COUNT/$TOTAL_COUNT tests (excluding $((TOTAL_COUNT - REGULAR_COUNT)) process-isolated)"

# Run tests
bun test \
    ${COVERAGE:+--coverage --coverage-reporter=lcov --coverage-dir=coverage} \
    $REGULAR_TESTS
