#!/bin/bash
# Run tests that conflict with centralized preloads or use inline mock.module() calls.
# Issue #1323: Process isolation via /tmp to avoid bunfig.toml loading.
#
# Five groups: A (minimal preloads), B (with registry/context), C (inline mocks),
# D (real-implementation hooks), E (MerchantsTab inline DataTable mock). Each runs as a
# separate bun process to prevent pollution.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"

COVERAGE=false
for arg in "$@"; do
    case $arg in
        --coverage) COVERAGE=true; shift ;;
    esac
done

echo "Running process-isolated test groups (A, B, C, D, E, F, G, H)..."
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

# Group B: With registry/dashboard-context preloads (DashboardGrid, WidgetCatalog,
# ScopeSegmentedControl). ScopeSegmentedControl.test.tsx (Issue #1370) joins this group
# for the same reason as DashboardGrid/WidgetCatalog: DashboardPage.test.tsx inline-mocks
# './ScopeSegmentedControl' (relative path, same resolved module id as this file's own
# import), and bun's mock.module() is process-global — running both in the same process
# means DashboardPage.test.tsx's stub leaks into this file's "real component" tests.
# household-api.ts and track-event.ts are added here (beyond DashboardGrid/WidgetCatalog's
# original needs) because ScopeSegmentedControl calls the real useHousehold() (needs
# useGetMyHousehold/useGetHouseholdEntitlement mocked) and fires a PostHog analytics
# event via useTrackEvent() on selection.
GROUP_B_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/widgets/DashboardGrid.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/WidgetCatalog.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/components/ScopeSegmentedControl.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/components/ScopeSegmentedControl.privacy.test.tsx"
)
GROUP_B_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/registry.ts" \
    "$FRONTEND_DIR/src/test/mocks/track-event.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-context.ts" \
    "$FRONTEND_DIR/src/test/mocks/dashboard-wrapper-hooks.ts" \
    "$FRONTEND_DIR/src/test/mocks/accounts-api.ts" \
    "$FRONTEND_DIR/src/test/mocks/household-api.ts"; do
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
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"; do
    GROUP_C_PRELOAD_ARGS+=(--preload "$f")
done

# Group D: Real-implementation hook tests
GROUP_D_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/hooks/useDashboardData.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/hooks/useNetWorthData.test.tsx"
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

# Group F: the deleted-goals CACHE regression (#1569, Codex finding 4). It must run in a process
# where src/test/mocks/savings-goals-api.ts has NEVER been loaded: that mock replaces
# useGetSavingsGoals / useGetDeletedSavingsGoals, and a mocked hook has no cache, so the very
# defect the test exists to catch would be unobservable. It cannot join Group D for exactly that
# reason — Group D preloads savings-goals-api.ts.
GROUP_F_FILES=(
    "$FRONTEND_DIR/src/features/savings/hooks/useSavingsGoalsDeletedCache.test.tsx"
)
GROUP_F_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx" \
    "$FRONTEND_DIR/src/test/mocks/posthog.ts" \
    "$FRONTEND_DIR/src/test/mocks/track-event.ts" \
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"; do
    GROUP_F_PRELOAD_ARGS+=(--preload "$f")
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
    "$FRONTEND_DIR/src/test/mocks/sonner.ts"; do
    GROUP_E_PRELOAD_ARGS+=(--preload "$f")
done

# Group G: SankeyViewModeControl (#1626) — must run WITHOUT the mocks/toggle-group.tsx preload so
# it exercises the REAL @radix-ui/react-toggle-group. That mock does not forward an item's
# aria-label, so under the shared bunfig-preloaded process this file could only prove the wrapper
# forwards an aria-label, never that SankeyViewModeControl actually supplies one. Its own group
# (not folded into Group A, which already runs the real toggle-group but lacks the track-event
# preload) because the control calls useTrackEvent(), which without a mock drags in the real
# PostHog client.
GROUP_G_FILES=(
    "$FRONTEND_DIR/src/features/reports/components/SankeyViewModeControl.test.tsx"
)
GROUP_G_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts" \
    "$FRONTEND_DIR/src/test/testing-library.ts" \
    "$FRONTEND_DIR/src/test/mocks/track-event.ts"; do
    GROUP_G_PRELOAD_ARGS+=(--preload "$f")
done

# Group H: real PostHog SDK. The centralized posthog preload replaces the SDK module,
# so this privacy boundary needs its own process to exercise actual sessionStorage behavior.
GROUP_H_FILES=(
    "$FRONTEND_DIR/src/shared/lib/posthog-real-sdk.test.ts"
)
GROUP_H_PRELOAD_ARGS=()
for f in \
    "$FRONTEND_DIR/src/test/happydom.ts"; do
    GROUP_H_PRELOAD_ARGS+=(--preload "$f")
done

run_group A "Minimal preloads" "${GROUP_A_PRELOAD_ARGS[@]}" "${GROUP_A_FILES[@]}"
run_group B "Registry/context" "${GROUP_B_PRELOAD_ARGS[@]}" "${GROUP_B_FILES[@]}"
run_group C "Inline mocks" "${GROUP_C_PRELOAD_ARGS[@]}" "${GROUP_C_FILES[@]}"
run_group D "Real hooks" "${GROUP_D_PRELOAD_ARGS[@]}" "${GROUP_D_FILES[@]}"
run_group E "MerchantsTab inline DataTable mock" "${GROUP_E_PRELOAD_ARGS[@]}" "${GROUP_E_FILES[@]}"
run_group F "Real savings-goals hooks + real query cache" "${GROUP_F_PRELOAD_ARGS[@]}" "${GROUP_F_FILES[@]}"
run_group G "SankeyViewModeControl against real Radix toggle-group" "${GROUP_G_PRELOAD_ARGS[@]}" "${GROUP_G_FILES[@]}"
run_group H "Real PostHog SDK sessionStorage privacy" "${GROUP_H_PRELOAD_ARGS[@]}" "${GROUP_H_FILES[@]}"

# Group I: Inline mock.module() widget/budget tests + useCashFlowData isolated per-file from
# $FRONTEND_DIR so they inherit bunfig.toml preloads without polluting test-regular.sh.
GROUP_I_FILES=(
    "src/features/dashboard/hooks/useCashFlowData.test.tsx"
    "src/features/dashboard/widgets/AccountCardsWidget.test.tsx"
    "src/features/dashboard/widgets/BalanceHistoryWidget.test.tsx"
    "src/features/dashboard/widgets/BalanceOverviewWidget.test.tsx"
    "src/features/budgets/components/category-management/BudgetCategoryCard.test.tsx"
    "src/features/budgets/components/category-management/BudgetCategoryManager.test.tsx"
)
idx=1
for f in "${GROUP_I_FILES[@]}"; do
    echo "Running Group I-$idx: $f..."
    if [ "$COVERAGE" = true ]; then
        (cd "$FRONTEND_DIR" && bun test --timeout 30000 \
            --coverage --coverage-reporter=lcov \
            --coverage-dir="$FRONTEND_DIR/coverage-conflict/group-i-$idx" \
            "$f")
    else
        (cd "$FRONTEND_DIR" && bun test --timeout 30000 "$f")
    fi
    idx=$((idx + 1))
done

echo "Process-isolated test groups completed!"
