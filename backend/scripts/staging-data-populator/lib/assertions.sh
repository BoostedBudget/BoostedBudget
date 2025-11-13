#!/bin/bash

# assertions.sh - Validation framework for scenarios
# Provides extensible assertion types for verifying scenario results

# Run all assertions defined in scenario
run_assertions() {
  log_section "ASSERTIONS"

  local count
  count=$(get_assertions_count)

  if [ "$count" = "0" ] || [ "$count" = "null" ]; then
    log_info "No assertions defined in scenario"
    return 0
  fi

  log_info "Running $count assertion(s)..."

  local passed=0
  local failed=0

  for i in $(seq 0 $((count - 1))); do
    local type
    type=$(get_assertion_type "$i")

    case "$type" in
      "transaction_count")
        if assert_transaction_count "$i"; then
          ((passed++))
        else
          ((failed++))
        fi
        ;;
      "account_count")
        if assert_account_count "$i"; then
          ((passed++))
        else
          ((failed++))
        fi
        ;;
      "user_tier")
        if assert_user_tier "$i"; then
          ((passed++))
        else
          ((failed++))
        fi
        ;;
      "balance_sum")
        if assert_balance_sum "$i"; then
          ((passed++))
        else
          ((failed++))
        fi
        ;;
      *)
        log_error "Unknown assertion type: $type"
        ((failed++))
        ;;
    esac
  done

  echo ""
  if [ $failed -eq 0 ]; then
    log_success "Assertions: $passed passed, $failed failed"
    return 0
  else
    log_error "Assertions: $passed passed, $failed failed"
    return 1
  fi
}

# Assert transaction count
assert_transaction_count() {
  local index=$1
  local operator
  operator=$(get_assertion_operator "$index")
  local expected
  expected=$(get_assertion_value "$index")

  local actual
  actual=$(count_transactions_by_user_id "$USER_ID")

  local result
  case "$operator" in
    ">=")
      if [ "$actual" -ge "$expected" ]; then
        result=0
      else
        result=1
      fi
      ;;
    "=")
      if [ "$actual" -eq "$expected" ]; then
        result=0
      else
        result=1
      fi
      ;;
    "<=")
      if [ "$actual" -le "$expected" ]; then
        result=0
      else
        result=1
      fi
      ;;
    ">")
      if [ "$actual" -gt "$expected" ]; then
        result=0
      else
        result=1
      fi
      ;;
    "<")
      if [ "$actual" -lt "$expected" ]; then
        result=0
      else
        result=1
      fi
      ;;
    *)
      log_error "Unknown operator: $operator"
      return 1
      ;;
  esac

  if [ $result -eq 0 ]; then
    log_success "Transaction count: $actual $operator $expected"
    return 0
  else
    log_error "Transaction count: $actual NOT $operator $expected"
    return 1
  fi
}

# Assert account count
assert_account_count() {
  local index=$1
  local operator
  operator=$(get_assertion_operator "$index")
  local expected
  expected=$(get_assertion_value "$index")

  local actual
  actual=$(count_accounts_by_user_id "$USER_ID")

  local result
  case "$operator" in
    ">=")
      [[ $actual -ge $expected ]] && result=0 || result=1
      ;;
    "=")
      [[ $actual -eq $expected ]] && result=0 || result=1
      ;;
    "<=")
      [[ $actual -le $expected ]] && result=0 || result=1
      ;;
    ">")
      [[ $actual -gt $expected ]] && result=0 || result=1
      ;;
    "<")
      [[ $actual -lt $expected ]] && result=0 || result=1
      ;;
    *)
      log_error "Unknown operator: $operator"
      return 1
      ;;
  esac

  if [ $result -eq 0 ]; then
    log_success "Account count: $actual $operator $expected"
    return 0
  else
    log_error "Account count: $actual NOT $operator $expected"
    return 1
  fi
}

# Assert user tier
assert_user_tier() {
  local index=$1
  local expected
  expected=$(get_assertion_expected "$index")

  local actual
  actual=$(get_user_tier_by_id "$USER_ID")

  if [ "$actual" = "$expected" ]; then
    log_success "User tier: $actual = $expected"
    return 0
  else
    log_error "User tier: $actual != $expected"
    return 1
  fi
}

# Assert balance sum
assert_balance_sum() {
  local index=$1
  local operator
  operator=$(get_assertion_operator "$index")
  local expected
  expected=$(get_assertion_value "$index")

  local actual
  actual=$(sum_account_balances_by_user_id "$USER_ID")

  # Compare floats (remove decimals for integer comparison)
  local actual_int=$(echo "$actual" | awk '{printf "%.0f", $1}')
  local expected_int=$(echo "$expected" | awk '{printf "%.0f", $1}')

  local result
  case "$operator" in
    ">=")
      [[ $actual_int -ge $expected_int ]] && result=0 || result=1
      ;;
    "=")
      [[ $actual_int -eq $expected_int ]] && result=0 || result=1
      ;;
    "<=")
      [[ $actual_int -le $expected_int ]] && result=0 || result=1
      ;;
    ">")
      [[ $actual_int -gt $expected_int ]] && result=0 || result=1
      ;;
    "<")
      [[ $actual_int -lt $expected_int ]] && result=0 || result=1
      ;;
    *)
      log_error "Unknown operator: $operator"
      return 1
      ;;
  esac

  if [ $result -eq 0 ]; then
    log_success "Balance sum: $actual $operator $expected"
    return 0
  else
    log_error "Balance sum: $actual NOT $operator $expected"
    return 1
  fi
}
