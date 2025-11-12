#!/bin/bash

# setup-items.sh - Linked items creation for fake data mode
# Creates fake linked_items with fake Plaid access tokens

# Global arrays set by setup_linked_items
declare -ag LINKED_ITEM_IDS
declare -ag LINKED_ITEM_PLAID_IDS

# Create linked items from scenario
setup_linked_items() {
  log_info "Setting up linked items..."

  local count
  count=$(get_linked_items_count)

  if [ "$count" = "0" ] || [ "$count" = "null" ]; then
    log_warning "No linked items defined in scenario"
    return 0
  fi

  LINKED_ITEM_IDS=()
  LINKED_ITEM_PLAID_IDS=()

  for i in $(seq 0 $((count - 1))); do
    local institution
    institution=$(get_linked_item_institution "$i")

    if [ "$institution" = "null" ] || [ -z "$institution" ]; then
      log_warning "Institution not specified for linked_items[$i], using 'Unknown Bank'"
      institution="Unknown Bank"
    fi

    # Generate IDs
    local item_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    local plaid_item_id="fake-item-$(uuidgen | cut -d'-' -f1)"
    local access_token="fake-sandbox-token-$(uuidgen)"

    log_debug "Creating linked_item[$i]:"
    log_debug "  Institution: $institution"
    log_debug "  ID: $item_id"
    log_debug "  Plaid Item ID: $plaid_item_id"

    # Insert linked item
    local sql="
      INSERT INTO linked_items (
        id, user_id, item_id, institution_name,
        access_token_encrypted, cursor, created_at, updated_at
      )
      VALUES (
        '$item_id',
        '$USER_ID',
        '$plaid_item_id',
        '$institution',
        '$access_token',
        NULL,
        NOW(),
        NOW()
      );
    "

    if execute_db "$sql"; then
      LINKED_ITEM_IDS+=("$item_id")
      LINKED_ITEM_PLAID_IDS+=("$plaid_item_id")
      log_progress "$((i + 1))" "$count" "Linked item: $institution"
    else
      log_error "Failed to create linked_item[$i]"
      return 1
    fi
  done

  log_success "Created $count linked item(s)"
  return 0
}

# Verify linked items were created
verify_linked_items_created() {
  local expected_count=${#LINKED_ITEM_IDS[@]}
  local actual_count
  actual_count=$(count_linked_items_by_user_id "$USER_ID")

  if [ "$actual_count" -eq "$expected_count" ]; then
    log_debug "Linked items verified: $actual_count"
    return 0
  else
    log_error "Linked items verification failed: expected $expected_count, got $actual_count"
    return 1
  fi
}
