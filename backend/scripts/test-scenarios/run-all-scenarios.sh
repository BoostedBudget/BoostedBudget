#!/bin/bash
# BoostedBudget Test Scenarios - Batch Runner
# Executes multiple scenario files sequentially or in parallel

set -e  # Exit on error

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source libraries for logging
source "$SCRIPT_DIR/lib/api-client.sh"

# ============================================================================
# Configuration
# ============================================================================

ENV_NAME="${ENV_NAME:-staging}"
PARALLEL_MODE="${PARALLEL_MODE:-false}"
CONTINUE_ON_FAILURE="${CONTINUE_ON_FAILURE:-false}"
REPORT_DIR="${REPORT_OUTPUT_DIR:-$SCRIPT_DIR/reports}"
SUMMARY_REPORT="$REPORT_DIR/batch-summary-$(date +%Y%m%d-%H%M%S).txt"

# Scenario categories
SCENARIO_DIRS=(
    "$SCRIPT_DIR/scenarios"
)

# Default scenario pattern (all .yaml files)
SCENARIO_PATTERN="${SCENARIO_PATTERN:-*.yaml}"

# Results tracking
TOTAL_SCENARIOS=0
PASSED_SCENARIOS=0
FAILED_SCENARIOS=0
FAILED_SCENARIO_LIST=()

# ============================================================================
# Usage
# ============================================================================

usage() {
    cat <<EOF
Usage: $0 [options]

Runs multiple test scenarios in batch mode.

Options:
  --env <name>           Environment (staging|local) [default: staging]
  --parallel             Run scenarios in parallel (experimental)
  --continue-on-failure  Continue running even if scenarios fail
  --pattern <pattern>    Scenario file pattern [default: *.yaml]
  --category <name>      Run only scenarios in specific category
  --help                 Show this help message

Environment Variables:
  API_BASE_URL           API base URL (required)
  AUTH_TOKEN             Auth0 JWT token (required)
  DATABASE_URL           PostgreSQL connection string (required)
  PARALLEL_MODE          Run in parallel [default: false]
  CONTINUE_ON_FAILURE    Continue on failure [default: false]
  SCENARIO_PATTERN       File pattern [default: *.yaml]

Examples:
  # Run all scenarios sequentially
  $0

  # Run all scenarios in parallel (faster but harder to debug)
  $0 --parallel

  # Run only tier limit scenarios
  $0 --pattern "*tier*.yaml"

  # Continue even if some fail (for CI/CD)
  $0 --continue-on-failure

  # Run against local environment
  $0 --env local

EOF
}

# ============================================================================
# Argument Parsing
# ============================================================================

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --help)
                usage
                exit 0
                ;;
            --env)
                ENV_NAME="$2"
                shift 2
                ;;
            --parallel)
                PARALLEL_MODE="true"
                shift
                ;;
            --continue-on-failure)
                CONTINUE_ON_FAILURE="true"
                shift
                ;;
            --pattern)
                SCENARIO_PATTERN="$2"
                shift 2
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# ============================================================================
# Scenario Discovery
# ============================================================================

find_scenarios() {
    local scenarios=()

    for dir in "${SCENARIO_DIRS[@]}"; do
        if [[ -d "$dir" ]]; then
            while IFS= read -r -d '' file; do
                scenarios+=("$file")
            done < <(find "$dir" -name "$SCENARIO_PATTERN" -type f -print0 | sort -z)
        fi
    done

    echo "${scenarios[@]}"
}

# ============================================================================
# Scenario Execution
# ============================================================================

run_single_scenario() {
    local scenario_file="$1"
    local scenario_name=$(basename "$scenario_file" .yaml)

    log_info "Running scenario: $scenario_name"

    # Run scenario
    if "$SCRIPT_DIR/run-scenario.sh" "$scenario_file" --env "$ENV_NAME"; then
        log_success "✓ PASSED: $scenario_name"
        return 0
    else
        log_error "✗ FAILED: $scenario_name"
        return 1
    fi
}

run_scenarios_sequential() {
    local scenarios=("$@")

    log_info "Running ${#scenarios[@]} scenarios sequentially..."
    echo ""

    for scenario_file in "${scenarios[@]}"; do
        local scenario_name=$(basename "$scenario_file" .yaml)

        ((TOTAL_SCENARIOS++))

        if run_single_scenario "$scenario_file"; then
            ((PASSED_SCENARIOS++))
        else
            ((FAILED_SCENARIOS++))
            FAILED_SCENARIO_LIST+=("$scenario_name")

            if [[ "$CONTINUE_ON_FAILURE" != "true" ]]; then
                log_error "Stopping due to failure (use --continue-on-failure to continue)"
                break
            fi
        fi

        echo ""
        echo "----------------------------------------"
        echo ""
    done
}

run_scenarios_parallel() {
    local scenarios=("$@")

    log_info "Running ${#scenarios[@]} scenarios in parallel..."
    echo ""

    local pids=()

    # Start all scenarios in background
    for scenario_file in "${scenarios[@]}"; do
        ((TOTAL_SCENARIOS++))

        run_single_scenario "$scenario_file" &
        pids+=($!)
    done

    # Wait for all to complete
    for pid in "${pids[@]}"; do
        if wait "$pid"; then
            ((PASSED_SCENARIOS++))
        else
            ((FAILED_SCENARIOS++))
        fi
    done
}

# ============================================================================
# Summary Report
# ============================================================================

generate_summary_report() {
    mkdir -p "$REPORT_DIR"

    cat > "$SUMMARY_REPORT" <<EOF
========================================
  BATCH SCENARIO EXECUTION SUMMARY
========================================

Environment:  $ENV_NAME
Execution Mode: $([ "$PARALLEL_MODE" == "true" ] && echo "Parallel" || echo "Sequential")
Start Time:   $(date -u +"%Y-%m-%d %H:%M:%S UTC")

----------------------------------------
RESULTS
----------------------------------------

Total Scenarios:  $TOTAL_SCENARIOS
Passed:           $PASSED_SCENARIOS
Failed:           $FAILED_SCENARIOS
Success Rate:     $(awk "BEGIN {printf \"%.1f%%\", ($PASSED_SCENARIOS / $TOTAL_SCENARIOS) * 100}")

EOF

    if [[ ${#FAILED_SCENARIO_LIST[@]} -gt 0 ]]; then
        echo "----------------------------------------" >> "$SUMMARY_REPORT"
        echo "FAILED SCENARIOS" >> "$SUMMARY_REPORT"
        echo "----------------------------------------" >> "$SUMMARY_REPORT"
        for failed in "${FAILED_SCENARIO_LIST[@]}"; do
            echo "  - $failed" >> "$SUMMARY_REPORT"
        done
        echo "" >> "$SUMMARY_REPORT"
    fi

    echo "========================================" >> "$SUMMARY_REPORT"
    echo "End Time: $(date -u +"%Y-%m-%d %H:%M:%S UTC")" >> "$SUMMARY_REPORT"
    echo "========================================" >> "$SUMMARY_REPORT"

    # Print summary to console
    cat "$SUMMARY_REPORT"

    log_info "Summary report saved to: $SUMMARY_REPORT"
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    # Parse arguments
    parse_args "$@"

    # Print header
    echo ""
    echo "========================================"
    echo "  BATCH SCENARIO RUNNER"
    echo "========================================"
    echo "Environment: $ENV_NAME"
    echo "Mode: $([ "$PARALLEL_MODE" == "true" ] && echo "Parallel" || echo "Sequential")"
    echo "========================================"
    echo ""

    # Find scenarios
    local scenarios=($(find_scenarios))

    if [[ ${#scenarios[@]} -eq 0 ]]; then
        log_error "No scenarios found matching pattern: $SCENARIO_PATTERN"
        exit 1
    fi

    log_info "Found ${#scenarios[@]} scenario(s)"
    echo ""

    # List scenarios
    for scenario in "${scenarios[@]}"; do
        echo "  - $(basename "$scenario")"
    done
    echo ""

    # Run scenarios
    if [[ "$PARALLEL_MODE" == "true" ]]; then
        run_scenarios_parallel "${scenarios[@]}"
    else
        run_scenarios_sequential "${scenarios[@]}"
    fi

    # Generate summary
    echo ""
    generate_summary_report

    # Exit with appropriate code
    if [[ $FAILED_SCENARIOS -gt 0 ]]; then
        exit 1
    else
        exit 0
    fi
}

# Run main
main "$@"
