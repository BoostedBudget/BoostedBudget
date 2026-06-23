#!/bin/bash
# Run tests that conflict with preloaded mocks in bunfig.toml
# Issue #1323: Migrate to single bun test src/ command
#
# These 3 test files test the REAL implementation of modules that are
# preloaded as mocks in bunfig.toml. They cannot run in the main batch
# because the preload has already replaced the real module with a mock.
#
# Solution: Run from /tmp to avoid loading bunfig.toml (Bun always loads
# bunfig.toml from CWD; --config is ADDITIVE, not a replacement).
# Pass only the minimal preloads each file actually needs.
#
# Test files included:
# - DashboardContext.test.tsx  (needs real useDashboardContext; dashboard-context.ts preload conflicts)
# - registry.test.ts           (needs real widget registry; registry.ts preload conflicts)
# - useInfiniteInvestmentTransactions.test.tsx (needs real hook; investments-hooks.ts preload conflicts)

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

# The 3 preload-conflict test files (absolute paths)
TEST_FILES=(
    "$FRONTEND_DIR/src/features/dashboard/context/DashboardContext.test.tsx"
    "$FRONTEND_DIR/src/features/dashboard/widgets/registry.test.ts"
    "$FRONTEND_DIR/src/features/investments/hooks/useInfiniteInvestmentTransactions.test.tsx"
)

# Minimal preloads: DOM, testing-library, and only what these files need
# - clerk.tsx: DashboardContext -> useActiveAccounts -> useUser() needs a provider stub
# - investments-api.ts: useInfiniteInvestmentTransactions mocks the raw fetcher (not the wrapper hook)
# Deliberately NO wrapper-level mocks (dashboard-context.ts, registry.ts, investments-hooks.ts)
PRELOAD_FILES=(
    "$FRONTEND_DIR/src/test/happydom.ts"
    "$FRONTEND_DIR/src/test/testing-library.ts"
    "$FRONTEND_DIR/src/test/mocks/clerk.tsx"
    "$FRONTEND_DIR/src/test/mocks/investments-api.ts"
)

# Build preload arguments
PRELOAD_ARGS=$(printf -- '--preload %s ' "${PRELOAD_FILES[@]}")

# CRITICAL: Run from /tmp to avoid loading bunfig.toml from frontend CWD.
# Bun's --config flag is ADDITIVE, not a replacement. Running from /tmp
# ensures no bunfig.toml is found, so only our explicit preloads apply.
cd /tmp

if [ "$COVERAGE" = true ]; then
    echo "Coverage enabled - generating to coverage-conflict/"
    bun test $PRELOAD_ARGS \
        --timeout 30000 \
        --coverage --coverage-reporter=lcov \
        --coverage-dir="$FRONTEND_DIR/coverage-conflict" \
        "${TEST_FILES[@]}"
else
    bun test $PRELOAD_ARGS --timeout 30000 "${TEST_FILES[@]}"
fi

echo ""
echo "Preload-conflict tests completed!"
