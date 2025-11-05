#!/bin/bash
# BoostedBudget Test Scenarios - Main Scenario Runner
# Executes YAML scenario files against staging/local environment

set -e  # Exit on error

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source libraries
source "$SCRIPT_DIR/lib/api-client.sh"
source "$SCRIPT_DIR/lib/db-utils.sh"
source "$SCRIPT_DIR/lib/assertions.sh"
source "$SCRIPT_DIR/lib/fixtures.sh"
source "$SCRIPT_DIR/lib/reporting.sh"

# ============================================================================
# Configuration
# ============================================================================

SCENARIO_FILE=""
ENV_NAME="${ENV_NAME:-staging}"
CLEANUP_AFTER="${CLEANUP_AFTER_TEST:-true}"
REPORT_DIR="${REPORT_OUTPUT_DIR:-$SCRIPT_DIR/reports}"

# Ensure report directory exists
mkdir -p "$REPORT_DIR"

# ============================================================================
# Usage
# ============================================================================

usage() {
    cat <<EOF
Usage: $0 <scenario_file> [options]

Arguments:
  scenario_file           Path to YAML scenario file (required)

Options:
  --env <name>           Environment (staging|local) [default: staging]
  --no-cleanup           Skip cleanup after test
  --report-dir <dir>     Report output directory [default: ./reports]
  -h, --help             Show this help message

Environment Variables:
  API_BASE_URL           API base URL (required)
  AUTH_TOKEN             Auth0 JWT token (required)
  DATABASE_URL           PostgreSQL connection string (required)
  CLEANUP_AFTER_TEST     Cleanup after test [default: true]
  REPORT_OUTPUT_DIR      Report output directory [default: ./reports]

Examples:
  # Run scenario against staging
  $0 scenarios/01-free-tier-first-sync.yaml

  # Run without cleanup
  $0 scenarios/01-free-tier-first-sync.yaml --no-cleanup

  # Run against local environment
  $0 scenarios/01-free-tier-first-sync.yaml --env local

EOF
}

# ============================================================================
# Argument Parsing
# ============================================================================

parse_args() {
    if [[ $# -eq 0 ]]; then
        usage
        exit 1
    fi

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                usage
                exit 0
                ;;
            --env)
                ENV_NAME="$2"
                shift 2
                ;;
            --no-cleanup)
                CLEANUP_AFTER="false"
                shift
                ;;
            --report-dir)
                REPORT_DIR="$2"
                shift 2
                ;;
            -*)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
            *)
                if [[ -z "$SCENARIO_FILE" ]]; then
                    SCENARIO_FILE="$1"
                else
                    log_error "Multiple scenario files specified"
                    usage
                    exit 1
                fi
                shift
                ;;
        esac
    done

    if [[ -z "$SCENARIO_FILE" ]]; then
        log_error "Scenario file is required"
        usage
        exit 1
    fi

    if [[ ! -f "$SCENARIO_FILE" ]]; then
        log_error "Scenario file not found: $SCENARIO_FILE"
        exit 1
    fi
}

# ============================================================================
# Validation
# ============================================================================

validate_environment() {
    log_info "Validating environment: $ENV_NAME"

    # Validate API environment
    if ! validate_api_env; then
        exit 1
    fi

    # Validate database environment
    if ! validate_db_env; then
        exit 1
    fi

    # Test database connection
    if ! test_db_connection; then
        exit 1
    fi

    # Check if yq is available for YAML parsing
    if ! command -v yq &>/dev/null; then
        log_error "yq is required for YAML parsing"
        log_error "Install: https://github.com/mikefarah/yq"
        exit 1
    fi

    log_success "Environment validation passed"
}

# ============================================================================
# YAML Parsing
# ============================================================================

# Parse scenario metadata
parse_scenario_metadata() {
    local scenario_file="$1"

    SCENARIO_NAME=$(yq eval '.scenario.name' "$scenario_file")
    SCENARIO_DESCRIPTION=$(yq eval '.scenario.description' "$scenario_file")
    SCENARIO_CATEGORY=$(yq eval '.scenario.category' "$scenario_file")

    log_info "Scenario: $SCENARIO_NAME"
    log_info "Description: $SCENARIO_DESCRIPTION"
    log_info "Category: $SCENARIO_CATEGORY"
}

# ============================================================================
# Setup Phase
# ============================================================================

setup_test_data() {
    local scenario_file="$1"

    log_info "Setting up test data..."

    # Parse user
    local user_id=$(yq eval '.scenario.setup.user.id' "$scenario_file")
    local user_tier=$(yq eval '.scenario.setup.user.tier' "$scenario_file")
    local user_email=$(yq eval '.scenario.setup.user.email' "$scenario_file")

    if [[ "$user_id" != "null" ]]; then
        log_info "Creating user: $user_id ($user_tier)"
        insert_user "$user_id" "$user_tier" "$user_email" || {
            log_error "Failed to create user"
            return 1
        }
    fi

    # Parse and create linked items
    local item_count=$(yq eval '.scenario.setup.linked_items | length' "$scenario_file")
    log_info "Creating $item_count linked item(s)..."

    for ((i=0; i<item_count; i++)); do
        local item_id=$(yq eval ".scenario.setup.linked_items[$i].id" "$scenario_file")
        local item_user_id=$(yq eval ".scenario.setup.linked_items[$i].user_id" "$scenario_file")
        local plaid_item_id=$(yq eval ".scenario.setup.linked_items[$i].item_id" "$scenario_file")
        local institution_name=$(yq eval ".scenario.setup.linked_items[$i].institution_name" "$scenario_file")
        local cursor=$(yq eval ".scenario.setup.linked_items[$i].cursor" "$scenario_file")
        local status=$(yq eval ".scenario.setup.linked_items[$i].status" "$scenario_file")

        # Handle null cursor
        if [[ "$cursor" == "null" ]]; then
            cursor="NULL"
        fi

        # Default status
        if [[ "$status" == "null" ]]; then
            status="active"
        fi

        insert_linked_item "$item_id" "$item_user_id" "$plaid_item_id" "$institution_name" "$cursor" "$status" || {
            log_error "Failed to create linked item: $item_id"
            return 1
        }
    done

    # Parse and create accounts
    local account_count=$(yq eval '.scenario.setup.accounts | length' "$scenario_file")
    log_info "Creating $account_count account(s)..."

    for ((i=0; i<account_count; i++)); do
        local account_id=$(yq eval ".scenario.setup.accounts[$i].id" "$scenario_file")
        local linked_item_id=$(yq eval ".scenario.setup.accounts[$i].linked_item_id" "$scenario_file")
        local account_user_id=$(yq eval ".scenario.setup.accounts[$i].user_id" "$scenario_file")
        local plaid_account_id=$(yq eval ".scenario.setup.accounts[$i].plaid_account_id" "$scenario_file")
        local name=$(yq eval ".scenario.setup.accounts[$i].name" "$scenario_file")
        local type=$(yq eval ".scenario.setup.accounts[$i].type" "$scenario_file")
        local subtype=$(yq eval ".scenario.setup.accounts[$i].subtype" "$scenario_file")
        local balance=$(yq eval ".scenario.setup.accounts[$i].current_balance" "$scenario_file")

        insert_account "$account_id" "$linked_item_id" "$account_user_id" "$plaid_account_id" "$name" "$type" "$subtype" "$balance" || {
            log_error "Failed to create account: $account_id"
            return 1
        }
    done

    # Parse and create pre-existing transactions (if any)
    local txn_count=$(yq eval '.scenario.setup.transactions | length' "$scenario_file" 2>/dev/null || echo "0")
    if [[ "$txn_count" != "null" ]] && [[ "$txn_count" -gt 0 ]]; then
        log_info "Creating $txn_count pre-existing transaction(s)..."

        for ((i=0; i<txn_count; i++)); do
            local txn_user_id=$(yq eval ".scenario.setup.transactions[$i].user_id" "$scenario_file")
            local txn_account_id=$(yq eval ".scenario.setup.transactions[$i].account_id" "$scenario_file")
            local txn_linked_item_id=$(yq eval ".scenario.setup.transactions[$i].linked_item_id" "$scenario_file")
            local external_id=$(yq eval ".scenario.setup.transactions[$i].external_transaction_id" "$scenario_file")
            local amount=$(yq eval ".scenario.setup.transactions[$i].amount" "$scenario_file")
            local date=$(yq eval ".scenario.setup.transactions[$i].date" "$scenario_file")
            local txn_name=$(yq eval ".scenario.setup.transactions[$i].name" "$scenario_file")
            local user_notes=$(yq eval ".scenario.setup.transactions[$i].user_notes" "$scenario_file")

            # Handle null user_notes
            if [[ "$user_notes" == "null" ]]; then
                user_notes="NULL"
            fi

            insert_transaction "$txn_user_id" "$txn_account_id" "$txn_linked_item_id" "$external_id" "$amount" "$date" "$txn_name" "$user_notes" || {
                log_error "Failed to create transaction: $external_id"
                return 1
            }
        done
    fi

    log_success "Test data setup complete"
    add_report_metadata "test_data_setup" "success"
}

# ============================================================================
# Execution Phase
# ============================================================================

execute_steps() {
    local scenario_file="$1"

    log_info "Executing scenario steps..."

    local step_count=$(yq eval '.scenario.steps | length' "$scenario_file")
    log_info "Total steps: $step_count"

    for ((i=0; i<step_count; i++)); do
        local step_num=$((i + 1))
        local action=$(yq eval ".scenario.steps[$i].action" "$scenario_file")

        log_info "Step $step_num: $action"

        case "$action" in
            fire_webhook)
                local webhook_code=$(yq eval ".scenario.steps[$i].webhook_code" "$scenario_file")
                local item_id=$(yq eval ".scenario.steps[$i].item_id" "$scenario_file")

                log_info "  Firing webhook: $webhook_code for item $item_id"
                if fire_webhook "$item_id" "$webhook_code" >/dev/null; then
                    add_report_step "$step_num" "fire_webhook ($webhook_code)" "success" "item_id: $item_id"
                else
                    add_report_step "$step_num" "fire_webhook ($webhook_code)" "failed" "item_id: $item_id"
                    log_error "  Failed to fire webhook"
                    return 1
                fi
                ;;

            wait)
                local wait_seconds=$(yq eval ".scenario.steps[$i].wait_seconds" "$scenario_file")
                log_info "  Waiting ${wait_seconds}s for processing..."
                sleep "$wait_seconds"
                add_report_step "$step_num" "wait" "success" "duration: ${wait_seconds}s"
                ;;

            query_db)
                local query=$(yq eval ".scenario.steps[$i].query" "$scenario_file")
                log_info "  Executing custom query..."
                if query_db "$query" >/dev/null; then
                    add_report_step "$step_num" "query_db" "success" "query executed"
                else
                    add_report_step "$step_num" "query_db" "failed" "query failed"
                    log_error "  Query failed"
                    return 1
                fi
                ;;

            *)
                log_warning "  Unknown action: $action (skipping)"
                add_report_step "$step_num" "$action" "skipped" "unknown action"
                ;;
        esac
    done

    log_success "All steps executed"
}

# ============================================================================
# Assertion Phase
# ============================================================================

run_assertions() {
    local scenario_file="$1"

    log_info "Running assertions..."
    reset_assertions

    # Database assertions
    if yq eval '.scenario.assertions.database' "$scenario_file" | grep -q -v "null"; then
        log_info "Running database assertions..."

        # Transaction count
        local txn_count_assertion=$(yq eval '.scenario.assertions.database.transactions_count' "$scenario_file")
        if [[ "$txn_count_assertion" != "null" ]]; then
            local user_id=$(yq eval '.scenario.setup.user.id' "$scenario_file")
            local actual_count=$(get_transaction_count "$user_id")

            # Parse assertion (e.g., ">= 1" or "10")
            if [[ "$txn_count_assertion" =~ ^[0-9]+$ ]]; then
                # Exact match
                assert_equal "$actual_count" "$txn_count_assertion" "Transaction count should be $txn_count_assertion"
            elif [[ "$txn_count_assertion" =~ ^\>\=\ ([0-9]+)$ ]]; then
                # Greater than or equal
                local threshold="${BASH_REMATCH[1]}"
                assert_gte "$actual_count" "$threshold" "Transaction count should be >= $threshold"
            fi
        fi

        # Sync audit count
        local sync_audit_assertion=$(yq eval '.scenario.assertions.database.sync_audits_count' "$scenario_file")
        if [[ "$sync_audit_assertion" != "null" ]]; then
            local user_id=$(yq eval '.scenario.setup.user.id' "$scenario_file")
            local month=$(date +%Y-%m)
            local actual_count=$(get_sync_audit_count "$user_id" "$month")

            if [[ "$sync_audit_assertion" =~ ^[0-9]+$ ]]; then
                assert_equal "$actual_count" "$sync_audit_assertion" "Sync audit count should be $sync_audit_assertion"
            fi
        fi
    fi

    # Webhook status assertions
    if yq eval '.scenario.assertions.webhook_status' "$scenario_file" | grep -q -v "null"; then
        log_info "Running webhook status assertions..."

        local item_id=$(yq eval '.scenario.setup.linked_items[0].item_id' "$scenario_file")

        # Processed assertion
        local processed_assertion=$(yq eval '.scenario.assertions.webhook_status.processed' "$scenario_file")
        if [[ "$processed_assertion" == "true" ]]; then
            assert_webhook_processed "$item_id"
        fi

        # Error assertion
        local error_assertion=$(yq eval '.scenario.assertions.webhook_status.error' "$scenario_file")
        if [[ "$error_assertion" != "null" ]]; then
            assert_webhook_has_error "$item_id" "$error_assertion"
        fi
    fi

    # Print summary
    print_assertion_summary

    # Store results in report
    set_assertion_counts "$ASSERTIONS_PASSED" "$ASSERTIONS_FAILED" "${ASSERTION_FAILURES[@]}"

    # Return 0 if all passed, 1 if any failed
    if [[ $ASSERTIONS_FAILED -eq 0 ]]; then
        return 0
    else
        return 1
    fi
}

# ============================================================================
# Cleanup Phase
# ============================================================================

cleanup_test_data() {
    local scenario_file="$1"

    if [[ "$CLEANUP_AFTER" != "true" ]]; then
        log_warning "Skipping cleanup (--no-cleanup specified)"
        return 0
    fi

    log_info "Cleaning up test data..."

    local user_id=$(yq eval '.scenario.setup.user.id' "$scenario_file")

    if [[ "$user_id" != "null" ]]; then
        cleanup_test_user "$user_id" || {
            log_warning "Cleanup failed for user: $user_id"
        }
    fi

    log_success "Cleanup complete"
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    # Parse arguments
    parse_args "$@"

    # Parse scenario metadata
    parse_scenario_metadata "$SCENARIO_FILE"

    # Initialize report
    init_report "$SCENARIO_NAME"
    add_report_metadata "scenario_file" "$SCENARIO_FILE"
    add_report_metadata "environment" "$ENV_NAME"

    # Validate environment
    validate_environment

    # Execute scenario
    local scenario_status="success"

    # Setup
    if ! setup_test_data "$SCENARIO_FILE"; then
        scenario_status="failure"
        log_error "Setup phase failed"
    fi

    # Execute steps (only if setup succeeded)
    if [[ "$scenario_status" == "success" ]]; then
        if ! execute_steps "$SCENARIO_FILE"; then
            scenario_status="failure"
            log_error "Execution phase failed"
        fi
    fi

    # Run assertions (only if execution succeeded)
    if [[ "$scenario_status" == "success" ]]; then
        if ! run_assertions "$SCENARIO_FILE"; then
            scenario_status="failure"
            log_error "Assertion phase failed"
        fi
    fi

    # Cleanup
    cleanup_test_data "$SCENARIO_FILE"

    # Finalize report
    finalize_report "$scenario_status"

    # Generate reports
    local report_basename="$(basename "$SCENARIO_FILE" .yaml)-$(date +%Y%m%d-%H%M%S)"
    generate_json_report "$REPORT_DIR/${report_basename}.json"
    generate_html_report "$REPORT_DIR/${report_basename}.html"

    # Create symlinks to latest
    ln -sf "${report_basename}.json" "$REPORT_DIR/latest.json"
    ln -sf "${report_basename}.html" "$REPORT_DIR/latest.html"

    # Print summary
    print_report_summary

    # Exit with appropriate code
    if [[ "$scenario_status" == "success" ]]; then
        log_success "Scenario completed successfully"
        exit 0
    else
        log_error "Scenario failed"
        exit 1
    fi
}

# Run main
main "$@"
