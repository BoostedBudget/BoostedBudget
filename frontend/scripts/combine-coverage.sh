#!/bin/bash
# Combine coverage reports from multiple bun test runs
# Issue #575: Supports coverage from both main tests and real-implementation tests
#
# Usage: ./scripts/combine-coverage.sh [--check-threshold]
#
# This script:
# 1. Looks for coverage files in coverage/ and coverage-real/
# 2. Merges lcov files into coverage/combined.lcov
# 3. Optionally checks thresholds from vitest.config.ts (lines: 25, functions: 67, branches: 68)

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
if [ -f "coverage/lcov.info" ]; then
    LCOV_FILES="$LCOV_FILES coverage/lcov.info"
    echo "Found: coverage/lcov.info"
fi
if [ -f "coverage-real/lcov.info" ]; then
    LCOV_FILES="$LCOV_FILES coverage-real/lcov.info"
    echo "Found: coverage-real/lcov.info"
fi

if [ -z "$LCOV_FILES" ]; then
    echo "❌ No coverage files found!"
    echo "Run tests with --coverage flag first."
    exit 1
fi

# Check if lcov is available for merging
if command -v lcov &> /dev/null; then
    echo ""
    echo "Merging coverage files with lcov..."

    # Build lcov merge command
    LCOV_CMD="lcov"
    for f in $LCOV_FILES; do
        LCOV_CMD="$LCOV_CMD -a $f"
    done
    LCOV_CMD="$LCOV_CMD -o coverage/combined.lcov"

    $LCOV_CMD

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

        # Thresholds from vitest.config.ts
        LINES_THRESHOLD=25
        FUNCTIONS_THRESHOLD=67
        BRANCHES_THRESHOLD=68

        FAILED=false

        if [ -n "$LINES" ]; then
            LINES_INT=$(echo "$LINES" | cut -d. -f1)
            if [ "$LINES_INT" -lt "$LINES_THRESHOLD" ]; then
                echo "❌ Lines coverage ${LINES}% is below threshold ${LINES_THRESHOLD}%"
                FAILED=true
            else
                echo "✅ Lines coverage ${LINES}% meets threshold ${LINES_THRESHOLD}%"
            fi
        fi

        if [ -n "$FUNCTIONS" ]; then
            FUNCTIONS_INT=$(echo "$FUNCTIONS" | cut -d. -f1)
            if [ "$FUNCTIONS_INT" -lt "$FUNCTIONS_THRESHOLD" ]; then
                echo "❌ Functions coverage ${FUNCTIONS}% is below threshold ${FUNCTIONS_THRESHOLD}%"
                FAILED=true
            else
                echo "✅ Functions coverage ${FUNCTIONS}% meets threshold ${FUNCTIONS_THRESHOLD}%"
            fi
        fi

        if [ -n "$BRANCHES" ]; then
            BRANCHES_INT=$(echo "$BRANCHES" | cut -d. -f1)
            if [ "$BRANCHES_INT" -lt "$BRANCHES_THRESHOLD" ]; then
                echo "❌ Branches coverage ${BRANCHES}% is below threshold ${BRANCHES_THRESHOLD}%"
                FAILED=true
            else
                echo "✅ Branches coverage ${BRANCHES}% meets threshold ${BRANCHES_THRESHOLD}%"
            fi
        fi

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
    genhtml coverage/combined.lcov -o coverage/html --quiet
    echo "✅ HTML report: coverage/html/index.html"
fi

echo ""
echo "========================================"
echo "✅ Coverage combination complete!"
echo "========================================"
