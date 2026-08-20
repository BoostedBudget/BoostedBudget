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
    "src/features/dashboard/components/ScopeSegmentedControl.test.tsx"
    "src/components/ui/toggle-group.test.tsx"
    # Inline mock.module() pollution (see feedback_bun_test_mock_module_global.md)
    "src/features/accounts/AccountsPage.test.tsx"
    "src/features/auto-categorization/components/MerchantsTab.test.tsx"
    # Real-implementation hook tests
    "src/features/dashboard/hooks/useDashboardData.test.tsx"
    "src/features/dashboard/hooks/useNetWorthData.test.tsx"
    "src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    "src/features/dashboard/hooks/useGettingStarted.test.ts"
    "src/features/budgets/hooks/useBudgetFilters.test.ts"
    "src/features/budgets/hooks/useBudgets.test.tsx"
    "src/features/savings/hooks/useSavingsGoals.test.tsx"
    "src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "src/features/budgets/hooks/useAvailableCategories.test.tsx"
    "src/features/transactions/hooks/useInfiniteTransactions.test.tsx"
    "src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "src/features/budgets/hooks/usePreviewCategories.test.tsx"
    # #898: useSessionDismiss tests the real implementation; session-dismiss.ts preload would shadow it
    "src/shared/hooks/useSessionDismiss.test.ts"
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
# --timeout is passed EXPLICITLY and must stay that way. bunfig.toml declares
# `[test] timeout = 30000`, and bun 1.4.0 ignores it — verified with a 6s probe test, which
# times out at exactly 5000ms (bun's built-in default) under bunfig alone and passes with
# this flag. So the 30s the repo has declared since bunfig was written has never actually
# applied to this script.
#
# Its sibling test-conflict-files.sh already passed `--timeout 30000` on both of its
# invocations, so the workaround existed — it just was not applied here, to the 362-file
# suite that does the bulk of the work. That inconsistency is what failed CI on #1537:
# CategoryEditor's "should close creation form and dropdown after successful creation"
# takes 441ms locally and 9910ms on a loaded runner, and died against the 5s default.
bun test \
    --timeout 30000 \
    ${COVERAGE:+--coverage --coverage-reporter=lcov --coverage-dir=coverage} \
    $REGULAR_TESTS
