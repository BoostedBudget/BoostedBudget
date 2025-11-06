#!/bin/bash
# Assertions Library for BoostedBudget Test Scenarios
# Provides assertion functions for validating test outcomes

# Source logging and db functions if not already loaded
if ! type -t log_info &>/dev/null; then
    source "$(dirname "${BASH_SOURCE[0]}")/api-client.sh"
fi

if ! type -t query_db &>/dev/null; then
    source "$(dirname "${BASH_SOURCE[0]}")/db-utils.sh"
fi

# Track assertion results
ASSERTIONS_PASSED=0
ASSERTIONS_FAILED=0
ASSERTION_FAILURES=()

# Reset assertion counters
reset_assertions() {
    ASSERTIONS_PASSED=0
    ASSERTIONS_FAILED=0
    ASSERTION_FAILURES=()
}

# Assert equal
# Usage: assert_equal <actual> <expected> <message>
assert_equal() {
    local actual="$1"
    local expected="$2"
    local message="${3:-Assertion failed}"

    if [[ "$actual" == "$expected" ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Expected: '$expected'"
        log_error "  Actual:   '$actual'"
        ASSERTION_FAILURES+=("$message: expected '$expected', got '$actual'")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert not equal
# Usage: assert_not_equal <actual> <unexpected> <message>
assert_not_equal() {
    local actual="$1"
    local unexpected="$2"
    local message="${3:-Assertion failed}"

    if [[ "$actual" != "$unexpected" ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Should not equal: '$unexpected'"
        log_error "  But got:          '$actual'"
        ASSERTION_FAILURES+=("$message: should not equal '$unexpected'")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert greater than
# Usage: assert_gt <actual> <threshold> <message>
assert_gt() {
    local actual="$1"
    local threshold="$2"
    local message="${3:-Assertion failed}"

    if [[ "$actual" -gt "$threshold" ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Expected: > $threshold"
        log_error "  Actual:   $actual"
        ASSERTION_FAILURES+=("$message: expected > $threshold, got $actual")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert greater than or equal
# Usage: assert_gte <actual> <threshold> <message>
assert_gte() {
    local actual="$1"
    local threshold="$2"
    local message="${3:-Assertion failed}"

    if [[ "$actual" -ge "$threshold" ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Expected: >= $threshold"
        log_error "  Actual:   $actual"
        ASSERTION_FAILURES+=("$message: expected >= $threshold, got $actual")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert less than
# Usage: assert_lt <actual> <threshold> <message>
assert_lt() {
    local actual="$1"
    local threshold="$2"
    local message="${3:-Assertion failed}"

    if [[ "$actual" -lt "$threshold" ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Expected: < $threshold"
        log_error "  Actual:   $actual"
        ASSERTION_FAILURES+=("$message: expected < $threshold, got $actual")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert webhook processed
# Usage: assert_webhook_processed <item_id> [message]
assert_webhook_processed() {
    local item_id="$1"
    local message="${2:-Webhook should be processed for item $item_id}"

    local processed
    processed=$(query_db "SELECT processed FROM webhook_events WHERE item_id = '$item_id' ORDER BY created_at DESC LIMIT 1")

    assert_equal "$processed" "t" "$message"
}

# Assert webhook has error
# Usage: assert_webhook_has_error <item_id> <expected_error> [message]
assert_webhook_has_error() {
    local item_id="$1"
    local expected_error="$2"
    local message="${3:-Webhook should have error for item $item_id}"

    local error
    error=$(query_db "SELECT processing_error FROM webhook_events WHERE item_id = '$item_id' ORDER BY created_at DESC LIMIT 1")

    if [[ "$error" == *"$expected_error"* ]]; then
        log_success "✓ $message"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_error "✗ $message"
        log_error "  Expected error to contain: '$expected_error'"
        log_error "  Actual error:              '$error'"
        ASSERTION_FAILURES+=("$message")
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Assert transaction count
# Usage: assert_transaction_count <user_id> <expected_count> [message]
assert_transaction_count() {
    local user_id="$1"
    local expected_count="$2"
    local message="${3:-Transaction count for user $user_id should be $expected_count}"

    local actual_count
    actual_count=$(get_transaction_count "$user_id")

    assert_equal "$actual_count" "$expected_count" "$message"
}

# Assert transaction count greater than
# Usage: assert_transaction_count_gt <user_id> <threshold> [message]
assert_transaction_count_gt() {
    local user_id="$1"
    local threshold="$2"
    local message="${3:-Transaction count for user $user_id should be > $threshold}"

    local actual_count
    actual_count=$(get_transaction_count "$user_id")

    assert_gt "$actual_count" "$threshold" "$message"
}

# Assert sync audit count
# Usage: assert_sync_audit_count <user_id> <expected_count> [month] [message]
assert_sync_audit_count() {
    local user_id="$1"
    local expected_count="$2"
    local month="${3:-$(date +%Y-%m)}"
    local message="${4:-Sync audit count for user $user_id in $month should be $expected_count}"

    local actual_count
    actual_count=$(get_sync_audit_count "$user_id" "$month")

    assert_equal "$actual_count" "$expected_count" "$message"
}

# Assert user notes preserved
# Usage: assert_user_notes_preserved <external_transaction_id> <user_id> <expected_notes> [message]
assert_user_notes_preserved() {
    local external_id="$1"
    local user_id="$2"
    local expected_notes="$3"
    local message="${4:-User notes should be preserved for transaction $external_id}"

    local actual_notes
    actual_notes=$(get_user_notes "$external_id" "$user_id")

    assert_equal "$actual_notes" "$expected_notes" "$message"
}

# Assert linked item cursor updated
# Usage: assert_cursor_updated <item_id> <old_cursor> [message]
assert_cursor_updated() {
    local item_id="$1"
    local old_cursor="$2"
    local message="${3:-Cursor should be updated for item $item_id}"

    local new_cursor
    new_cursor=$(get_linked_item_cursor "$item_id")

    assert_not_equal "$new_cursor" "$old_cursor" "$message"
}

# Assert linked item status
# Usage: assert_linked_item_status <item_id> <expected_status> [message]
assert_linked_item_status() {
    local item_id="$1"
    local expected_status="$2"
    local message="${3:-Linked item $item_id status should be $expected_status}"

    local actual_status
    actual_status=$(get_linked_item_status "$item_id")

    assert_equal "$actual_status" "$expected_status" "$message"
}

# Assert notification exists
# Usage: assert_notification_exists <user_id> <type> [message]
assert_notification_exists() {
    local user_id="$1"
    local type="$2"
    local message="${3:-Notification of type '$type' should exist for user $user_id}"

    local count
    count=$(count_rows "notifications" "user_id = '$user_id' AND type = '$type'")

    assert_gt "$count" "0" "$message"
}

# Assert notification count
# Usage: assert_notification_count <user_id> <expected_count> [message]
assert_notification_count() {
    local user_id="$1"
    local expected_count="$2"
    local message="${3:-Notification count for user $user_id should be $expected_count}"

    local actual_count
    actual_count=$(count_rows "notifications" "user_id = '$user_id'")

    assert_equal "$actual_count" "$expected_count" "$message"
}

# Print assertion summary
# Usage: print_assertion_summary
# Returns: 0 if all passed, 1 if any failed
print_assertion_summary() {
    local total=$((ASSERTIONS_PASSED + ASSERTIONS_FAILED))

    echo ""
    echo "========================================"
    echo "         ASSERTION SUMMARY"
    echo "========================================"
    echo "Total:  $total"
    echo -e "${GREEN}Passed: $ASSERTIONS_PASSED${NC}"
    echo -e "${RED}Failed: $ASSERTIONS_FAILED${NC}"
    echo "========================================"

    if [[ $ASSERTIONS_FAILED -gt 0 ]]; then
        echo ""
        echo -e "${RED}FAILED ASSERTIONS:${NC}"
        for failure in "${ASSERTION_FAILURES[@]}"; do
            echo -e "  ${RED}✗${NC} $failure"
        done
        echo ""
        return 1
    else
        echo -e "${GREEN}All assertions passed!${NC}"
        echo ""
        return 0
    fi
}

# ============================================================================
# Budget Assertions (Phase 3+)
# ============================================================================
# NOTE: Budget features not yet implemented - these are future-ready

# Assert budget exists
# Usage: assert_budget_exists <budget_id> [message]
assert_budget_exists() {
    local budget_id="$1"
    local message="${2:-Budget $budget_id should exist}"

    local count=$(count_rows "budgets" "id = '$budget_id'")

    if [[ "$count" -eq 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (found $count)")
        log_error "✗ $message (found $count)"
    fi
}

# Assert budget spending matches expected amount
# Usage: assert_budget_spending <budget_id> <expected_amount> [message]
assert_budget_spending() {
    local budget_id="$1"
    local expected_amount="$2"
    local message="${3:-Budget $budget_id spending should be $expected_amount}"

    local actual_spending=$(query_db "SELECT current_spending FROM budgets WHERE id = '$budget_id'" | tail -n +3 | head -n 1 | xargs)

    if [[ -z "$actual_spending" ]]; then
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (budget not found)")
        log_error "✗ $message (budget not found)"
        return
    fi

    # Compare amounts (allow small floating point differences)
    local diff=$(awk "BEGIN {printf \"%.2f\", ($actual_spending - $expected_amount)}")
    local abs_diff=$(awk "BEGIN {printf \"%.2f\", ($diff < 0 ? -$diff : $diff)}")

    if (( $(awk "BEGIN {print ($abs_diff < 0.01)}") )); then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message (actual: $actual_spending)"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (actual: $actual_spending)")
        log_error "✗ $message (actual: $actual_spending)"
    fi
}

# Assert budget alert triggered at threshold
# Usage: assert_budget_alert_triggered <budget_id> <threshold> [message]
assert_budget_alert_triggered() {
    local budget_id="$1"
    local threshold="$2"
    local message="${3:-Budget $budget_id should have alert at ${threshold}% threshold}"

    # Check budget_alerts table for alert at this threshold
    local count=$(count_rows "budget_alerts" "budget_id = '$budget_id' AND threshold = $threshold")

    if [[ "$count" -ge 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message")
        log_error "✗ $message"
    fi
}

# Assert budget percentage matches expected
# Usage: assert_budget_percentage <budget_id> <expected_percentage> [message]
assert_budget_percentage() {
    local budget_id="$1"
    local expected_percentage="$2"
    local message="${3:-Budget $budget_id should be at ${expected_percentage}% spent}"

    # Calculate percentage from current_spending / limit
    local result=$(query_db "SELECT (current_spending / NULLIF(limit, 0)) * 100 as percentage FROM budgets WHERE id = '$budget_id'" | tail -n +3 | head -n 1 | xargs)

    if [[ -z "$result" ]]; then
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (budget not found)")
        log_error "✗ $message (budget not found)"
        return
    fi

    # Compare percentages
    local diff=$(awk "BEGIN {printf \"%.2f\", ($result - $expected_percentage)}")
    local abs_diff=$(awk "BEGIN {printf \"%.2f\", ($diff < 0 ? -$diff : $diff)}")

    if (( $(awk "BEGIN {print ($abs_diff < 1.0)}") )); then  # Within 1%
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message (actual: ${result}%)"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (actual: ${result}%)")
        log_error "✗ $message (actual: ${result}%)"
    fi
}

# Assert budget alert count
# Usage: assert_budget_alert_count <budget_id> <expected_count> [message]
assert_budget_alert_count() {
    local budget_id="$1"
    local expected_count="$2"
    local message="${3:-Budget $budget_id should have $expected_count alert(s)}"

    local actual_count=$(count_rows "budget_alerts" "budget_id = '$budget_id'")

    if [[ "$actual_count" -eq "$expected_count" ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (actual: $actual_count)")
        log_error "✗ $message (actual: $actual_count)"
    fi
}

# ============================================================================
# Rule Assertions (Phase 4+)
# ============================================================================
# NOTE: Rules framework not yet implemented - these are future-ready

# Assert rule exists
# Usage: assert_rule_exists <rule_id> [message]
assert_rule_exists() {
    local rule_id="$1"
    local message="${2:-Rule $rule_id should exist}"

    local count=$(count_rows "automation_rules" "id = '$rule_id'")

    if [[ "$count" -eq 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (found $count)")
        log_error "✗ $message (found $count)"
    fi
}

# Assert rule execution logged
# Usage: assert_rule_executed <rule_id> <transaction_id> [message]
assert_rule_executed() {
    local rule_id="$1"
    local transaction_id="$2"
    local message="${3:-Rule $rule_id should have executed for transaction $transaction_id}"

    # Check rule_executions audit table
    local count=$(count_rows "rule_executions" "rule_id = '$rule_id' AND transaction_id = '$transaction_id'")

    if [[ "$count" -ge 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message")
        log_error "✗ $message"
    fi
}

# Assert rule execution count
# Usage: assert_rule_execution_count <rule_id> <expected_count> [message]
assert_rule_execution_count() {
    local rule_id="$1"
    local expected_count="$2"
    local message="${3:-Rule $rule_id should have executed $expected_count time(s)}"

    local actual_count=$(count_rows "rule_executions" "rule_id = '$rule_id'")

    if [[ "$actual_count" -eq "$expected_count" ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (actual: $actual_count)")
        log_error "✗ $message (actual: $actual_count)"
    fi
}

# Assert rule action executed
# Usage: assert_rule_action_executed <rule_id> <transaction_id> <action_type> [message]
assert_rule_action_executed() {
    local rule_id="$1"
    local transaction_id="$2"
    local action_type="$3"
    local message="${4:-Rule $rule_id should have executed action '$action_type'}"

    # Check rule_execution_actions table for specific action
    local count=$(count_rows "rule_execution_actions" "rule_id = '$rule_id' AND transaction_id = '$transaction_id' AND action_type = '$action_type'")

    if [[ "$count" -ge 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message")
        log_error "✗ $message"
    fi
}

# Assert rule not executed (idempotency check)
# Usage: assert_rule_not_executed_twice <rule_id> <transaction_id> [message]
assert_rule_not_executed_twice() {
    local rule_id="$1"
    local transaction_id="$2"
    local message="${3:-Rule $rule_id should execute only once for transaction $transaction_id}"

    local execution_count=$(count_rows "rule_executions" "rule_id = '$rule_id' AND transaction_id = '$transaction_id'")

    if [[ "$execution_count" -eq 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message (executed exactly once)"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (executed $execution_count times)")
        log_error "✗ $message (executed $execution_count times)"
    fi
}

# Assert transaction auto-categorized by rule
# Usage: assert_transaction_auto_categorized <transaction_id> <expected_category> [message]
assert_transaction_auto_categorized() {
    local transaction_id="$1"
    local expected_category="$2"
    local message="${3:-Transaction $transaction_id should be categorized as '$expected_category'}"

    local actual_category=$(query_db "SELECT category FROM transactions WHERE external_transaction_id = '$transaction_id'" | tail -n +3 | head -n 1 | xargs)

    if [[ "$actual_category" == "$expected_category" ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message (actual: $actual_category)")
        log_error "✗ $message (actual: $actual_category)"
    fi
}

# Assert transaction has tag
# Usage: assert_transaction_has_tag <transaction_id> <expected_tag> [message]
assert_transaction_has_tag() {
    local transaction_id="$1"
    local expected_tag="$2"
    local message="${3:-Transaction $transaction_id should have tag '$expected_tag'}"

    # Check transaction_tags table
    local count=$(count_rows "transaction_tags" "transaction_id = '$transaction_id' AND tag = '$expected_tag'")

    if [[ "$count" -ge 1 ]]; then
        ((ASSERTIONS_PASSED++))
        log_success "✓ $message"
    else
        ((ASSERTIONS_FAILED++))
        ASSERTION_FAILURES+=("$message")
        log_error "✗ $message"
    fi
}

# Export functions
export -f reset_assertions
export -f assert_equal
export -f assert_not_equal
export -f assert_gt
export -f assert_gte
export -f assert_lt
export -f assert_webhook_processed
export -f assert_webhook_has_error
export -f assert_transaction_count
export -f assert_transaction_count_gt
export -f assert_sync_audit_count
export -f assert_user_notes_preserved
export -f assert_cursor_updated
export -f assert_linked_item_status
export -f assert_notification_exists
export -f assert_notification_count
export -f print_assertion_summary

# Budget assertions (Phase 3+)
export -f assert_budget_exists
export -f assert_budget_spending
export -f assert_budget_alert_triggered
export -f assert_budget_percentage
export -f assert_budget_alert_count

# Rule assertions (Phase 4+)
export -f assert_rule_exists
export -f assert_rule_executed
export -f assert_rule_execution_count
export -f assert_rule_action_executed
export -f assert_rule_not_executed_twice
export -f assert_transaction_auto_categorized
export -f assert_transaction_has_tag

# Export variables
export ASSERTIONS_PASSED
export ASSERTIONS_FAILED
export ASSERTION_FAILURES
