#!/bin/bash
# Run tests that conflict with centralized preloads or use inline mock.module() calls.
# Issue #1323: Process isolation via /tmp to avoid bunfig.toml loading.
#
# Four groups: A (minimal preloads), B (with registry/context), C (inline mocks),
# D (real-implementation hooks). Each runs as a separate bun process to prevent pollution.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"

COVERAGE=false
for arg in "$@"; do
    case $arg in
        --coverage) COVERAGE=true; shift ;;
    esac
done

echo "Running process-isolated test groups (A, B, C, D)..."
cd /tmp

run_group() {
    local name=$1 label=$2
    shift 2
    echo "Running Group $name: $label..."
    if [ "$COVERAGE" = true ]; then
        local group_name
        group_name=$(echo "$name" | tr '[:upper:]' '[:lower:]')
        bun test --timeout 30000 \
            --coverage --coverage-reporter=lcov \
            --coverage-dir="$FRONTEND_DIR/coverage-conflict/group-$group_name" \
            "$@"
    else
        bun test --timeout 30000 "$@"
    fi
}

# Group A: Minimal preloads (DashboardContext, registry, etc.)
GROUP_A_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/context/DashboardContext.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/registry.test.ts"
    "$FRONTEND_DIR/src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "$FRONTEND_DIR/src/components/ui/toggle-group.test.tsx"
)
GROUP_A_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/investments-api.ts"; do
    GROUP_A_PRELOAD_ARGS+=(--preload "$f")
done

# Group B: With registry/dashboard-context preloads (DashboardGrid, WidgetCatalog)
GROUP_B_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/WidgetCatalog.test.tsx"
)
GROUP_B_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/registry.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-wrapper-hooks.ts"; do
    GROUP_B_PRELOAD_ARGS+=(--preload "$f")
done

# Group C: Isolated inline mocks (AccountsPage)
GROUP_C_FILES=(
    "$FRONTEND_DIR/src/features/accounts/AccountsPage.test.tsx"
)
GROUP_C_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/registry.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-hooks.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-wrapper-hooks.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/analytics-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/tags-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/profile-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/subscription-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/user-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/accounts-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/posthog.ts" \
    "$FRONTEND_DIR/src/test/mocks/track-event.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/plaid-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/react-router.ts" \
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"; do
    GROUP_C_PRELOAD_ARGS+=(--preload "$f")
done

# Group D: Real-implementation hook tests
GROUP_D_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardData.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardLayout.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardOnboarding.test.ts"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useGettingStarted.test.ts"
    "$FRONTEND_DIR/src/shared/hooks/useSessionDismiss.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetFilters.test.ts"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgets.test.tsx"
    "$FRONTEND_DIR/src/features/savings/hooks/useSavingsGoals.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useBudgetDetail.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/useAvailableCategories.test.tsx"
    "$FRONTEND_DIR/src/features/transactions/hooks/useInfiniteTransactions.test.tsx"
    "$FRONTEND_DIR/src/features/budgets/hooks/usePreviewCategories.test.tsx"
)
GROUP_D_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/investments-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/savings-goals-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/budgets-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/boosts-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/auto-categorization-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts"; do
    GROUP_D_PRELOAD_ARGS+=(--preload "$f")
done

# Group E: MerchantsTab — uses inline mock.module('@/shared/components/DataTable'), which is
# process-global in bun:test. Must run isolated so the DataTable stub doesn't pollute
# TransactionTable.test.tsx and other real-DataTable consumers. See feedback_bun_test_mock_module_global.md.
GROUP_E_FILES=(
    "$FRONTEND_DIR/src/features/auto-categorization/components/MerchantsTab.test.tsx"
)
GROUP_E_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/merchants-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/track-event.ts" \
    "$FRONTEND_DIR/src/test/mocks/posthog.ts" \
    "$FRONTEND_DIR/src/test/mocks/react-router.ts" \
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"; do
    GROUP_E_PRELOAD_ARGS+=(--preload "$f")
done

run_group A "Minimal preloads" "${GROUP_A_PRELOAD_ARGS[@]}" "${GROUP_A_FILES[@]}"
run_group B "Registry/context" "${GROUP_B_PRELOAD_ARGS[@]}" "${GROUP_B_FILES[@]}"
run_group C "Inline mocks" "${GROUP_C_PRELOAD_ARGS[@]}" "${GROUP_C_FILES[@]}"
run_group D "Real hooks" "${GROUP_D_PRELOAD_ARGS[@]}" "${GROUP_D_FILES[@]}"
run_group E "MerchantsTab inline DataTable mock" "${GROUP_E_PRELOAD_ARGS[@]}" "${GROUP_E_FILES[@]}"

echo "Process-isolated test groups completed!"
