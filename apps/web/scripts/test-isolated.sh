#!/bin/bash
# Run isolated tests that need centralized mocks but are polluted in full suite
# Issue #575 / #1323
#
# Remaining isolated tests (after Task 4 fixes):
# - AccountsPage: uses local component mocks that can pollute AccountTable etc. (#839)
#
# Fixed and moved in Task 4:
# - BoostsManagePage → main batch (pollution source already resolved)
# - usePricingContext, usePostSignInCheckoutIntent → main batch (#1038)
# - TimeframeSelector, TransactionCard → main batch (afterEach cleanup was sufficient)
# - useGettingStarted → test-real-implementations.sh (now preload-mocked)
# - DashboardGrid, WidgetCatalog, toggle-group, DashboardContext, registry →
#   test-conflict-files.sh (need real implementations without preload mocks)

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
    # Issue #1323: DashboardGrid.test.tsx and WidgetCatalog.test.tsx → test-conflict-files.sh
    # Issue #1323 Task 4: BoostsManagePage FIXED — confirmed passes in full suite; moved to
    # main batch (bun test src/). Pollution source was already fixed.
    # AccountsPage uses inline mock.module('./AccountCard') and mock.module('./components/AccountTable')
    # which pollute AccountCard.test.tsx and AccountTable.test.tsx (confirmed: 34 failures in
    # AccountTable.test.tsx when run after AccountsPage.test.tsx in the same process).
    # Cannot centralize without creating new preload files AND moving AccountCard/AccountTable tests
    # to test-conflict-files.sh — out of scope for Task 4. Tracked in #839.
    "src/features/accounts/AccountsPage.test.tsx"
    # Issue #1323 Task 4: useGettingStarted.test.ts moved to test-real-implementations.sh.
    # Issue #1323 Task 4: TimeframeSelector.test.tsx and TransactionCard.test.tsx FIXED
    # (both already had afterEach setSystemTime() cleanup, confirmed pass in full suite).
    # These were moved to the main batch (test-regular.sh). Entries removed from here.
    # toggle-group.test.tsx moved to test-conflict-files.sh (Issue #1323 prior tasks).
    # Issue #1323 Task 4: usePricingContext and usePostSignInCheckoutIntent FIXED —
    # confirmed pass in full suite. Moved to main batch; entries removed from here (#1038).
    # Issue #1323: CustomCategoryManager, useCustomCategories, useCategoryMutations,
    # useReviewMutations, and CustomCategoryFilter FIXED — pollution sources
    # (BudgetCategoryManager, BudgetCategoryForm, AutoCategorizationPageWrapper) now
    # use centralized categories-api mock. These tests moved to main batch (bun test src/).
    # Keeping this comment for audit trail — see commit history for details.
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
