#!/bin/bash
# Combine coverage reports from multiple bun test runs
# Issue #575: Supports coverage from both main tests and real-implementation tests
#
# Usage: ./scripts/combine-coverage.sh [--check-threshold]
#
# This script:
# 1. Looks for coverage files in coverage/ and coverage-real/
# 2. Merges lcov files into coverage/combined.lcov
# 3. Optionally checks thresholds (lines: 25, functions: 67, branches: 68)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(dirname "$SCRIPT_DIR")"
cd "$FRONTEND_DIR"

CHECK_THRESHOLD=false
for arg in "$@"; do
    case $arg in
        --check-threshold)
            CHECK_THRESHOLD=true
            shift
            ;;
    esac
done

echo "========================================"
echo "Combining Frontend Coverage Reports"
echo "========================================"

# Ensure coverage directory exists
mkdir -p coverage

# Find all lcov files
LCOV_FILES=""
for dir in coverage coverage-real coverage-isolated; do
    if [ -f "$dir/lcov.info" ]; then
        LCOV_FILES="$LCOV_FILES $dir/lcov.info"
        echo "Found: $dir/lcov.info"
    fi
done

if [ -z "$LCOV_FILES" ]; then
    echo "❌ No coverage files found!"
    echo "Run tests with --coverage flag first."
    exit 1
fi

# Check if lcov is available for merging
if command -v lcov &> /dev/null; then
    echo ""
    echo "Merging coverage files with lcov..."

    # Build and execute lcov merge command
    lcov $(printf -- '-a %s ' $LCOV_FILES) -o coverage/combined.lcov

    echo "✅ Combined coverage written to coverage/combined.lcov"

    # Generate summary
    echo ""
    echo "Coverage Summary:"
    lcov --summary coverage/combined.lcov 2>&1 | grep -E "(lines|functions|branches)"

    if [ "$CHECK_THRESHOLD" = true ]; then
        echo ""
        echo "Checking coverage thresholds..."

        # Extract percentages from lcov summary
        SUMMARY=$(lcov --summary coverage/combined.lcov 2>&1)
        LINES=$(echo "$SUMMARY" | grep "lines" | grep -oE '[0-9]+\.[0-9]+%' | head -1 | sed 's/%//')
        FUNCTIONS=$(echo "$SUMMARY" | grep "functions" | grep -oE '[0-9]+\.[0-9]+%' | head -1 | sed 's/%//')
        BRANCHES=$(echo "$SUMMARY" | grep "branches" | grep -oE '[0-9]+\.[0-9]+%' | head -1 | sed 's/%//')

        # Coverage thresholds — FLOOR pattern, not ratchet (#1169).
        # Formula: THRESHOLD = max(floor(baseline), floor(measured) - 1).
        # All comparisons are integer-percent. Bumping the floor up is fine and welcome.
        # Lowering the floor requires a written reason in the PR description.
        # Measured 2026-05-21 (post-ECharts migration): lines=64.9%, functions=no data, branches=no data.
        # Baseline pre-migration (main): lines=25%, functions=67%, branches=68%.
        # functions/branches: Bun LCOV does not emit per-function/branch data; checks are
        # silently skipped for empty values. Thresholds retained for when the toolchain adds support.
        LINES_THRESHOLD=63
        FUNCTIONS_THRESHOLD=67
        BRANCHES_THRESHOLD=68

        FAILED=false

        # Helper function to check threshold
        check_threshold() {
            local metric=$1 value=$2 threshold=$3
            if [ -n "$value" ]; then
                local int_value=$(echo "$value" | cut -d. -f1)
                if [ "$int_value" -lt "$threshold" ]; then
                    echo "❌ $metric coverage ${value}% is below threshold ${threshold}%"
                    return 1
                else
                    echo "✅ $metric coverage ${value}% meets threshold ${threshold}%"
                fi
            fi
            return 0
        }

        check_threshold "Lines" "$LINES" "$LINES_THRESHOLD" || FAILED=true
        check_threshold "Functions" "$FUNCTIONS" "$FUNCTIONS_THRESHOLD" || FAILED=true
        check_threshold "Branches" "$BRANCHES" "$BRANCHES_THRESHOLD" || FAILED=true

        if [ "$FAILED" = true ]; then
            exit 1
        fi
    fi

else
    echo ""
    echo "⚠️  lcov not installed - using simple concatenation"
    echo "   Install lcov for proper merging: brew install lcov"

    # Simple concatenation (less accurate but works)
    cat $LCOV_FILES > coverage/combined.lcov
    echo "✅ Combined coverage written to coverage/combined.lcov (concatenated)"
fi

# Generate HTML report if genhtml is available
if command -v genhtml &> /dev/null; then
    echo ""
    echo "Generating HTML coverage report..."
    # genhtml can fail on source path mismatches (local vs CI workspace) — non-fatal
    if genhtml coverage/combined.lcov -o coverage/html --quiet 2>/dev/null; then
        echo "✅ HTML report: coverage/html/index.html"
    else
        echo "⚠️  HTML report skipped (source path resolution; threshold check still valid)"
    fi
fi

echo ""
echo "========================================"
echo "✅ Coverage combination complete!"
echo "========================================"
