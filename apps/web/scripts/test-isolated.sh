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
    # AccountsPage uses local component mocks that can pollute AccountTable etc. (#839)
    "src/features/accounts/AccountsPage.test.tsx"
    # useGettingStarted needs centralized mocks (dashboard-context, budgets-api, etc.)
    # but can't run in regular suite: DashboardPage.test.tsx mocks useGettingStarted globally (#848)
    "src/features/dashboard/hooks/useGettingStarted.test.ts"
    # ToggleGroup component test needs real module, but TimeframeSelector/DateRangePicker
    # tests mock @/components/ui/toggle-group globally in the regular suite (#953)
    "src/components/ui/toggle-group.test.tsx"
    # setSystemTime pollution: Bun's setSystemTime leaks across test files in the full suite.
    # These tests pass individually but fail when other date-mocking tests run first (#952)
    "src/features/dashboard/components/TimeframeSelector.test.tsx"
    "src/features/transactions/components/TransactionCard.test.tsx"
    # usePricingContext: mock state pollution from full suite causes tiers to appear loading (#1038)
    "src/features/pricing/hooks/usePricingContext.test.tsx"
    # usePostSignInCheckoutIntent: mockUseCreatePaddleCheckout state leaks from prior tests (#1038)
    "src/features/pricing/hooks/usePostSignInCheckoutIntent.test.tsx"
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
