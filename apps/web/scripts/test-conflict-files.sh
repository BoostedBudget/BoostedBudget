#!/bin/bash
# Run tests that conflict with preloaded mocks in bunfig.toml, or that use
# inline mock.module() calls which would pollute other tests in the same process,
# or that need REAL implementations (not centralized mocks).
# Issue #1323: Migrate to single bun test src/ command
#
# Solution: Run from /tmp to avoid loading bunfig.toml (Bun always loads
# bunfig.toml from CWD; --config is ADDITIVE, not a replacement).
# Pass only the minimal preloads each file actually needs.
#
# Group A (no registry/dashboard-context preloads):
# - DashboardContext.test.tsx  (needs real useDashboardContext; dashboard-context.ts preload conflicts)
# - registry.test.ts           (needs real widget registry; registry.ts preload conflicts)
# - useInfiniteInvestmentTransactions.test.tsx (needs real hook; investments-hooks.ts preload conflicts)
# - toggle-group.test.tsx      (needs real ToggleGroup; toggle-group.tsx preload conflicts — Issue #1323)
#
# Group B (WITH registry/dashboard-context preloads, NO dashboard-grid/widget-catalog):
# - DashboardGrid.test.tsx     (needs real DashboardGrid; dashboard-grid.tsx preload conflicts — Issue #1323)
# - WidgetCatalog.test.tsx     (needs real WidgetCatalog; widget-catalog.tsx preload conflicts — Issue #1323)
#
# Group C (inline mock.module() pollution — Issue #1323 final task):
# - AccountsPage.test.tsx      (inline mock.module() calls for AccountCard, AccountTable,
#                               AccountCustomizationModal, ConfirmationDialog, and hooks would
#                               pollute their respective test files if run in the same process)
#
# Group D (real-implementation hook tests — formerly test-real-implementations.sh):
# - useDashboardData.test.tsx, useDashboardLayout.test.tsx, useDashboardOnboarding.test.ts
# - useGettingStarted.test.ts, useBudgetFilters.test.ts, useBudgets.test.tsx
# - useSavingsGoals.test.tsx, useBudgetDetail.test.tsx, useAvailableCategories.test.tsx
# - useInfiniteTransactions.test.tsx, usePreviewCategories.test.tsx
# These need real hook implementations; centralized mocks in bunfig.toml would interfere.

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

echo "Running preload-conflict and real-implementation tests..."

# CRITICAL: Run from /tmp to avoid loading bunfig.toml from frontend CWD.
cd /tmp

# === Group A: Minimal preloads (no registry/dashboard-context mocks) ===
GROUP_A_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/context/DashboardContext.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/registry.test.ts"
    "$FRONTEND_DIR/src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "$FRONTEND_DIR/src/components/ui/toggle-group.test.tsx"
)

GROUP_A_PRELOADS=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx"
    "$FRONTEND_DIR/src/test/mocks/investments-api.ts"
)

GROUP_A_PRELOAD_ARGS=()
for f in "${GROUP_A_PRELOADS[@]}"; do
    GROUP_A_PRELOAD_ARGS+=(--preload "$f")
done

# === Group B: With registry/dashboard-context but WITHOUT dashboard-grid/widget-catalog ===
GROUP_B_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/WidgetCatalog.test.tsx"
)

GROUP_B_PRELOADS=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx"
    "$FRONTEND_DIR/src/test/mocks/registry.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-wrapper-hooks.ts"
)

GROUP_B_PRELOAD_ARGS=()
for f in "${GROUP_B_PRELOADS[@]}"; do
    GROUP_B_PRELOAD_ARGS+=(--preload "$f")
done

# === Group C: AccountsPage — inline mock.module() calls pollute sibling tests (Issue #1323) ===
# AccountsPage uses inline mock.module() for AccountCard, AccountTable, AccountCustomizationModal,
# ConfirmationDialog, useAccountFilters, and useAccountCustomization. Running from /tmp ensures
# these module replacements are process-isolated and cannot pollute other test files.
GROUP_C_FILES=(
    "$FRONTEND_DIR/src/features/accounts/AccountsPage.test.tsx"
)

GROUP_C_PRELOADS=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    "$FRONTEND_DIR/src/test/mocks/registry.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-hooks.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-wrapper-hooks.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-api.ts"
    "$FRONTEND_DIR/src/test/mocks/analytics-api.ts"
    "$FRONTEND_DIR/src/test/mocks/tags-api.ts"
    "$FRONTEND_DIR/src/test/mocks/profile-api.ts"
    "$FRONTEND_DIR/src/test/mocks/subscription-api.ts"
    "$FRONTEND_DIR/src/test/mocks/user-api.ts"
    "$FRONTEND_DIR/src/test/mocks/accounts-api.ts"
    "$FRONTEND_DIR/src/test/mocks/posthog.ts"
    "$FRONTEND_DIR/src/test/mocks/track-event.ts"
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx"
    "$FRONTEND_DIR/src/test/mocks/plaid-api.ts"
    "$FRONTEND_DIR/src/test/mocks/react-router.ts"
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"
)

GROUP_C_PRELOAD_ARGS=()
for f in "${GROUP_C_PRELOADS[@]}"; do
    GROUP_C_PRELOAD_ARGS+=(--preload "$f")
done

# === Group D: Real-implementation hook tests (formerly test-real-implementations.sh) ===
# These tests verify the REAL behavior of hooks that are mocked in centralized preloads.
# Only DOM, testing-library, and minimal API-level mocks are needed.
# NO wrapper-level mocks that would interfere with testing real hook implementations.
GROUP_D_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardData.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    # Issue #1323 Task 4: moved from test-isolated.sh; useGettingStarted now preload-mocked
    # so this test must run from /tmp to get the real hook implementation
    "$FRONTEND_DIR/src/features/dashboard/hooks/useGettingStarted.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetFilters.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgets.test.tsx"
    "$FRONTEND_DIR/src/features/savings/hooks/useSavingsGoals.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useAvailableCategories.test.tsx"
    # Hangs in the full parallel suite; --timeout kills the lingering event loop
    "$FRONTEND_DIR/src/features/transactions/hooks/useInfiniteTransactions.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/usePreviewCategories.test.tsx"
)

GROUP_D_PRELOADS=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    # Clerk mock: DashboardContext → useActiveAccounts → useUser() (Issue #1109 W4-D)
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx"
    # API-level mocks for hook tests (mock fetcher, not wrapper hook)
    "$FRONTEND_DIR/src/test/mocks/investments-api.ts"
    "$FRONTEND_DIR/src/test/mocks/savings-goals-api.ts"
    # Issue #1323 Task 4: additional mocks for useGettingStarted.test.ts
    # (moved from test-isolated.sh; needs API-level mocks but not wrapper-level mocks)
    "$FRONTEND_DIR/src/test/mocks/budgets-api.ts"
    "$FRONTEND_DIR/src/test/mocks/boosts-api.ts"
    "$FRONTEND_DIR/src/test/mocks/auto-categorization-api.ts"
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts"
)

GROUP_D_PRELOAD_ARGS=()
for f in "${GROUP_D_PRELOADS[@]}"; do
    GROUP_D_PRELOAD_ARGS+=(--preload "$f")
done

if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-conflict/"
    echo "Running Group A (DashboardContext, registry, useInfiniteInvestmentTransactions, toggle-group)..."
    bun test "${GROUP_A_PRELOAD_ARGS[@]}" \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-conflict" \
        "${GROUP_A_FILES[@]}"
    echo "Running Group B (DashboardGrid, WidgetCatalog)..."
    bun test "${GROUP_B_PRELOAD_ARGS[@]}" \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-conflict" \
        "${GROUP_B_FILES[@]}"
    echo "Running Group C (AccountsPage — isolated inline mocks)..."
    bun test "${GROUP_C_PRELOAD_ARGS[@]}" \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-conflict" \
        "${GROUP_C_FILES[@]}"
    echo "Running Group D (real-implementation hook tests)..."
    bun test "${GROUP_D_PRELOAD_ARGS[@]}" \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-conflict" \
        "${GROUP_D_FILES[@]}"
else
    echo "Running Group A (DashboardContext, registry, useInfiniteInvestmentTransactions, toggle-group)..."
    bun test "${GROUP_A_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_A_FILES[@]}"
    echo "Running Group B (DashboardGrid, WidgetCatalog)..."
    bun test "${GROUP_B_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_B_FILES[@]}"
    echo "Running Group C (AccountsPage — isolated inline mocks)..."
    bun test "${GROUP_C_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_C_FILES[@]}"
    echo "Running Group D (real-implementation hook tests)..."
    bun test "${GROUP_D_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_D_FILES[@]}"
fi

echo ""
echo "Preload-conflict and real-implementation tests completed!"
