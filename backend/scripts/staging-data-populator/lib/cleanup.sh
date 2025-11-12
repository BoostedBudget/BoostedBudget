#!/bin/bash

# cleanup.sh - Idempotent cleanup functions
# Ensures scenarios can be run multiple times safely

# Cleanup all data for a user by email
cleanup_by_email() {
  local email=$1

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

  # Delete user and all associated data
  if delete_user_by_email "$email"; then
    log_success "Cleanup complete: $email"
    return 0
  else
    log_error "Cleanup failed: $email"
    return 1
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
