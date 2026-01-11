#!/bin/bash
# Run tests that need REAL implementations (not centralized mocks)
# Issue #575: registry.test.ts and DashboardContext.test.tsx

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"
cd "$FRONTEND_DIR"

echo "Running real-implementation tests..."
echo "Temporarily swapping bunfig.toml for bunfig.real.toml"

# Backup current bunfig.toml
mv bunfig.toml bunfig.toml.bak

# Use the real-implementation config
cp bunfig.real.toml bunfig.toml

# Run the tests
set +e
bun test src/features/dashboard/widgets/registry.test.ts src/features/dashboard/context/DashboardContext.test.tsx
TEST_EXIT_CODE=$?
set -e

# Restore original bunfig.toml
mv bunfig.toml.bak bunfig.toml

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✓ Real-implementation tests passed!"
else
    echo "✗ Real-implementation tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE
