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
    # Issue #1323: DashboardGrid and WidgetCatalog moved to test-conflict-files.sh
    "src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "src/features/dashboard/widgets/WidgetCatalog.test.tsx"
    # Isolated tests polluted by other tests (run via test-isolated.sh)
    "src/features/boosts/components/BoostsManagePage.test.tsx"
    # AccountsPage uses local component mocks that can pollute AccountTable etc. (#839)
    "src/features/accounts/AccountsPage.test.tsx"
    # Issue #1323: toggle-group.test.tsx moved to test-conflict-files.sh; centralized mock
    # now in bunfig.toml preload so individual test files no longer need inline mocks.
    "src/components/ui/toggle-group.test.tsx"
    # Issue #1323: confirmation-dialog.test.tsx moved to test-conflict-files.sh; AccountsPage.test.tsx
    # registers a stub for @/components/ui/confirmation-dialog that pollutes this test.
    "src/components/ui/confirmation-dialog.test.tsx"
    # setSystemTime pollution: date mocks from other test files leak into these, causing
    # date mismatch failures in the full suite (#952)
    "src/features/dashboard/components/TimeframeSelector.test.tsx"
    "src/features/transactions/components/TransactionCard.test.tsx"
    # usePricingContext: mock state pollution from full suite causes tiers to appear loading (#1038)
    "src/features/pricing/hooks/usePricingContext.test.tsx"
    # usePostSignInCheckoutIntent: mockUseCreatePaddleCheckout state leaks from prior tests (#1038)
    "src/features/pricing/hooks/usePostSignInCheckoutIntent.test.tsx"
    # Issue #1323: CustomCategoryManager, useCustomCategories, useCategoryMutations,
    # useReviewMutations, and CustomCategoryFilter pollution FIXED — these tests now
    # run in the main batch. The pollution sources (BudgetCategoryManager,
    # BudgetCategoryForm, AutoCategorizationPageWrapper) were fixed to use the
    # centralized categories-api mock instead of overriding with inline mock.module().
    "src/features/transactions/hooks/useInfiniteTransactions.test.tsx"
    # Hangs in full suite (real React Query against mocked fetcher; same pattern as
    # useInfiniteTransactions — runs via test-real-implementations.sh instead)
    "src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "src/features/budgets/hooks/usePreviewCategories.test.tsx"
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
