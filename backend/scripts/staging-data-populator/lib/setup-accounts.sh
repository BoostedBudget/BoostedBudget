#!/bin/bash

# setup-accounts.sh - Accounts creation for fake data mode
# Creates fake accounts with fake Plaid account IDs

# Global arrays set by setup_accounts
declare -ag ACCOUNT_IDS
declare -ag ACCOUNT_PLAID_IDS
declare -ag ACCOUNT_LINKED_ITEM_IDS

# Create accounts from scenario
setup_accounts() {
  log_info "Setting up accounts..."

  local count
  count=$(get_accounts_count)

  if [ "$count" = "0" ] || [ "$count" = "null" ]; then
    log_warning "No accounts defined in scenario"
    return 0
  fi

  ACCOUNT_IDS=()
  ACCOUNT_PLAID_IDS=()
  ACCOUNT_LINKED_ITEM_IDS=()

  for i in $(seq 0 $((count - 1))); do
    local item_index
    item_index=$(get_account_item_index "$i")

    if [ "$item_index" = "null" ] || [ -z "$item_index" ]; then
      log_error "item_index required for accounts[$i]"
      return 1
    fi

    # Validate item_index
    if [ "$item_index" -ge "${#LINKED_ITEM_IDS[@]}" ]; then
      log_error "Invalid item_index $item_index for accounts[$i] (max: $((${#LINKED_ITEM_IDS[@]} - 1)))"
      return 1
    fi

    local type
    type=$(get_account_type "$i")
    local name
    name=$(get_account_name "$i")
    local balance
    balance=$(get_account_balance "$i")

    if [ "$type" = "null" ] || [ -z "$type" ]; then
      log_warning "Account type not specified for accounts[$i], using 'checking'"
      type="checking"
    fi

    if [ "$name" = "null" ] || [ -z "$name" ]; then
      name="Account $((i + 1))"
    fi

    if [ "$balance" = "null" ] || [ -z "$balance" ]; then
      balance="0.00"
    fi

    # Get linked_item_id for this account
    local linked_item_id="${LINKED_ITEM_IDS[$item_index]}"

    # Generate IDs
    local account_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    local plaid_account_id="fake-acc-$(uuidgen | cut -d'-' -f1)"

    log_debug "Creating account[$i]:"
    log_debug "  Name: $name"
    log_debug "  Type: $type"
    log_debug "  Balance: $balance"
    log_debug "  Linked Item: $item_index"

    # Insert account
    local sql="
      INSERT INTO accounts (
        id, linked_item_id, user_id, plaid_account_id,
        name, type, current_balance, created_at, updated_at
      )
      VALUES (
        '$account_id',
        '$linked_item_id',
        '$USER_ID',
        '$plaid_account_id',
        '$name',
        '$type',
        $balance,
        NOW(),
        NOW()
      );
    "

    if execute_db "$sql"; then
      ACCOUNT_IDS+=("$account_id")
      ACCOUNT_PLAID_IDS+=("$plaid_account_id")
      ACCOUNT_LINKED_ITEM_IDS+=("$linked_item_id")
      log_progress "$((i + 1))" "$count" "Account: $name ($type)"
    else
      log_error "Failed to create account[$i]"
      return 1
    fi
  done

  log_success "Created $count account(s)"
  return 0
}

# Verify accounts were created
verify_accounts_created() {
  local expected_count=${#ACCOUNT_IDS[@]}
  local actual_count
  actual_count=$(count_accounts_by_user_id "$USER_ID")

  if [ "$actual_count" -eq "$expected_count" ]; then
    log_debug "Accounts verified: $actual_count"
    return 0
  else
    log_error "Accounts verification failed: expected $expected_count, got $actual_count"
    return 1
  fi
}
