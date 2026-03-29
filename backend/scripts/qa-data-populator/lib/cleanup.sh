#!/bin/bash

# cleanup.sh - Idempotent cleanup functions
# Ensures scenarios can be run multiple times safely

# Cleanup user's data (accounts, transactions) but preserve the user
cleanup_user_data_only() {
  local email=$1

  if [ -z "$email" ]; then
    log_error "cleanup_user_data_only: email is required"
    return 1
  fi

  local user_id
  user_id=$(get_user_id_by_email "$email")

  if [ -z "$user_id" ]; then
    log_debug "User not found: $email"
    return 0
  fi

  log_debug "Cleaning up data for user: $user_id ($email)"

  # Delete only the user's data, NOT the user or user_identities
  execute_db "DELETE FROM transactions WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM accounts WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM linked_items WHERE user_id = '$user_id';" || return 1

  # Optional tables - may not exist yet
  execute_db "DELETE FROM subscription_overrides WHERE user_id = '$user_id';" 2>/dev/null || true
  execute_db "DELETE FROM balance_snapshots WHERE user_id = '$user_id';" 2>/dev/null || true

  log_debug "Data cleanup complete (user preserved): $email"
  return 0
}

# Cleanup all data for a user by email
cleanup_by_email() {
  local email=$1
  local preserve_user=${2:-false}

  if [ -z "$email" ]; then
    log_error "cleanup_by_email: email is required"
    return 1
  fi

  log_info "🗑️  Cleaning up existing data for: $email"

  # Check if user exists
  if ! user_exists "$email"; then
    log_debug "No existing data for: $email"
    return 0
  fi

  if [ "$preserve_user" = "true" ]; then
    # Preserve user (for existing mode) - only delete data
    log_debug "Preserving user account, cleaning data only"
    if cleanup_user_data_only "$email"; then
      log_success "Cleanup complete (user preserved): $email"
      return 0
    else
      log_error "Cleanup failed: $email"
      return 1
    fi
  else
    # Delete user and all associated data
    if delete_user_by_email "$email"; then
      log_success "Cleanup complete: $email"
      return 0
    else
      log_error "Cleanup failed: $email"
      return 1
    fi
  fi
}

# Cleanup all test data (scenarios with test-* or demo-* emails)
cleanup_all_test_data() {
  log_warning "Cleaning up all test/demo data..."

  local test_emails
  test_emails=$(query_db "SELECT email FROM users WHERE email LIKE 'test-%' OR email LIKE 'demo-%';" false)

  if [ -z "$test_emails" ]; then
    log_info "No test/demo data found"
    return 0
  fi

  local count=0
  while IFS= read -r email; do
    email=$(echo "$email" | tr -d ' ')
    if [ -n "$email" ]; then
      log_info "Cleaning up: $email"
      cleanup_by_email "$email"
      ((count++))
    fi
  done <<< "$test_emails"

  log_success "Cleaned up $count test/demo users"
}

# Verify cleanup (for testing)
verify_cleanup() {
  local email=$1

  if user_exists "$email"; then
    log_error "Cleanup verification failed: $email still exists"
    return 1
  fi

  log_debug "Cleanup verified: $email"
  return 0
}
