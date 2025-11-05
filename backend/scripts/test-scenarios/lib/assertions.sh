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

# Export variables
export ASSERTIONS_PASSED
export ASSERTIONS_FAILED
export ASSERTION_FAILURES
