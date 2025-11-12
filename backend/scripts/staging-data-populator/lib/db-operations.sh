#!/bin/bash

# db-operations.sh - PostgreSQL database helper functions
# Provides wrappers for common database operations

# Execute SQL query and return result
query_db() {
  local sql=$1
  local trim=${2:-true}

  log_debug "SQL: $sql"

  local result
  result=$(psql "$DATABASE_URL" -t -c "$sql" 2>&1)
  local exit_code=$?

  if [ $exit_code -ne 0 ]; then
    log_error "Database query failed:"
    log_error "$result"
    return 1
  fi

  if [ "$trim" = "true" ]; then
    echo "$result" | tr -d ' \n'
  else
    echo "$result"
  fi
}

# Execute SQL command (no output expected)
execute_db() {
  local sql=$1

  log_debug "SQL: $sql"

  local result
  result=$(psql "$DATABASE_URL" -c "$sql" 2>&1)
  local exit_code=$?

  if [ $exit_code -ne 0 ]; then
    log_error "Database command failed:"
    log_error "$result"
    return 1
  fi

  return 0
}

# Check if user exists by email
user_exists() {
  local email=$1

  local count
  count=$(query_db "SELECT COUNT(*) FROM users WHERE email = '$email';")

  if [ "$count" -gt 0 ]; then
    return 0
  else
    return 1
  fi
}

# Get user ID by email
get_user_id_by_email() {
  local email=$1

  local user_id
  user_id=$(query_db "SELECT id FROM users WHERE email = '$email';")

  if [ -z "$user_id" ]; then
    return 1
  fi

  echo "$user_id"
}

# Delete user and all associated data (cascade)
delete_user_by_email() {
  local email=$1

  local user_id
  user_id=$(get_user_id_by_email "$email")

  if [ -z "$user_id" ]; then
    log_debug "User not found: $email"
    return 0
  fi

  log_debug "Deleting user: $user_id ($email)"

  # Delete in reverse dependency order
  execute_db "DELETE FROM transactions WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM accounts WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM linked_items WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM subscription_overrides WHERE user_id = '$user_id';" || return 1
  execute_db "DELETE FROM users WHERE id = '$user_id';" || return 1

  return 0
}

# Count transactions for user
count_transactions_by_user_id() {
  local user_id=$1

  query_db "SELECT COUNT(*) FROM transactions WHERE user_id = '$user_id';"
}

# Count accounts for user
count_accounts_by_user_id() {
  local user_id=$1

  query_db "SELECT COUNT(*) FROM accounts WHERE user_id = '$user_id';"
}

# Count linked items for user
count_linked_items_by_user_id() {
  local user_id=$1

  query_db "SELECT COUNT(*) FROM linked_items WHERE user_id = '$user_id';"
}

# Get user tier
get_user_tier_by_id() {
  local user_id=$1

  query_db "SELECT subscription_tier FROM users WHERE id = '$user_id';"
}

# Sum account balances for user
sum_account_balances_by_user_id() {
  local user_id=$1

  query_db "SELECT COALESCE(SUM(current_balance), 0) FROM accounts WHERE user_id = '$user_id';"
}

# Validate database connection
validate_db_connection() {
  log_debug "Validating database connection..."

  local result
  result=$(psql "$DATABASE_URL" -t -c "SELECT 1;" 2>&1)
  local exit_code=$?

  if [ $exit_code -ne 0 ]; then
    log_error "Database connection failed:"
    log_error "$result"
    log_error "DATABASE_URL: ${DATABASE_URL:0:30}..."  # Show first 30 chars only
    return 1
  fi

  log_debug "Database connection successful"
  return 0
}

# Check if table exists
table_exists() {
  local table_name=$1

  local count
  count=$(query_db "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = '$table_name';")

  if [ "$count" -gt 0 ]; then
    return 0
  else
    return 1
  fi
}

# Validate required tables exist
validate_database_schema() {
  local required_tables=("users" "linked_items" "accounts" "transactions")

  for table in "${required_tables[@]}"; do
    if ! table_exists "$table"; then
      log_error "Required table '$table' not found in database"
      log_error "Please run migrations: cd backend && atlas migrate apply"
      return 1
    fi
  done

  log_debug "Database schema validated"
  return 0
}
