#!/bin/bash
# Run isolated tests that need centralized mocks but are polluted in full suite
# Issue #575 / #1323
#
# Issue #1323 (final task): ALL isolated tests have been fixed. This script is now
# empty and exists only for backward compatibility (CI/Makefile references).
#
# Fixed and moved in Task 4:
# - BoostsManagePage → main batch (pollution source already resolved)
# - usePricingContext, usePostSignInCheckoutIntent → main batch (#1038)
# - TimeframeSelector, TransactionCard → main batch (afterEach cleanup was sufficient)
# - useGettingStarted → test-real-implementations.sh (now preload-mocked)
# - DashboardGrid, WidgetCatalog, toggle-group, DashboardContext, registry →
#   test-conflict-files.sh (need real implementations without preload mocks)
#
# Fixed and moved in final task (#1323):
# - AccountsPage → test-conflict-files.sh Group C (inline mock.module() calls for
#   AccountCard, AccountTable, AccountCustomizationModal, ConfirmationDialog,
#   useAccountFilters, and useAccountCustomization are process-isolated from /tmp)

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

echo "Running isolated tests..."

# All previously isolated tests have been fixed and migrated:
# - AccountsPage → test-conflict-files.sh Group C (process-isolated from /tmp)
# See scripts/test-conflict-files.sh for the current configuration.
TEST_FILES=()

if [ ${#TEST_FILES[@]} -eq 0 ]; then
    echo "No isolated tests remaining — all tests have been migrated."
    echo "  - AccountsPage.test.tsx → test-conflict-files.sh Group C (inline mock isolation)"
    echo "test-isolated.sh is now empty. Issue #1323 complete."
    exit 0
fi

# Build test command
if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-isolated/"
    TEST_CMD="bun test --coverage --coverage-reporter=lcov --coverage-dir=coverage-isolated ${TEST_FILES[*]}"
else
    TEST_CMD="bun test ${TEST_FILES[*]}"
fi

# Run the tests
$TEST_CMD
