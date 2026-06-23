#!/bin/bash
# Run tests that conflict with preloaded mocks in bunfig.toml
# Issue #1323: Migrate to single bun test src/ command
#
# These test files test the REAL implementation of modules that are
# preloaded as mocks in bunfig.toml. They cannot run in the main batch
# because the preload has already replaced the real module with a mock.
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
# - confirmation-dialog.test.tsx (needs real ConfirmationDialog; AccountsPage.test.tsx registers a stub
#                                 via mock.module() before confirmation-dialog.test.tsx runs — Issue #1323)
#
# Group B (WITH registry/dashboard-context preloads, NO dashboard-grid/widget-catalog):
# - DashboardGrid.test.tsx     (needs real DashboardGrid; dashboard-grid.tsx preload conflicts — Issue #1323)
# - WidgetCatalog.test.tsx     (needs real WidgetCatalog; widget-catalog.tsx preload conflicts — Issue #1323)

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

echo "Running preload-conflict tests (real implementations)..."

# CRITICAL: Run from /tmp to avoid loading bunfig.toml from frontend CWD.
cd /tmp

# === Group A: Minimal preloads (no registry/dashboard-context mocks) ===
GROUP_A_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/context/DashboardContext.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/registry.test.ts"
    "$FRONTEND_DIR/src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
    "$FRONTEND_DIR/src/components/ui/toggle-group.test.tsx"
    "$FRONTEND_DIR/src/components/ui/confirmation-dialog.test.tsx"
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
else
    echo "Running Group A (DashboardContext, registry, useInfiniteInvestmentTransactions, toggle-group)..."
    bun test "${GROUP_A_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_A_FILES[@]}"
    echo "Running Group B (DashboardGrid, WidgetCatalog)..."
    bun test "${GROUP_B_PRELOAD_ARGS[@]}" --timeout 30000 "${GROUP_B_FILES[@]}"
fi

echo ""
echo "Preload-conflict tests completed!"
