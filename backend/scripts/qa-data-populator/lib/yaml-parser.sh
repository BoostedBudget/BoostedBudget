#!/bin/bash

# yaml-parser.sh - YAML parsing utilities using yq
# Provides helper functions for reading scenario YAML files

# Global variables set by parse_scenario (bash 3.2+ compatible)
SCENARIO_VERSION=""
SCENARIO_NAME=""
SCENARIO_DESCRIPTION=""
SCENARIO_TAGS=""

# Optional override variables (can be set by command-line)
OVERRIDE_USER_EMAIL="${OVERRIDE_USER_EMAIL:-}"
OVERRIDE_USER_MODE="${OVERRIDE_USER_MODE:-}"

# Parse and validate scenario YAML
parse_scenario() {
  local yaml_file=$1

  if [ ! -f "$yaml_file" ]; then
    log_error "Scenario file not found: $yaml_file"
    exit 1
  fi

  log_debug "Parsing scenario: $yaml_file"

  # Parse metadata
  SCENARIO_VERSION=$(yq e '.version' "$yaml_file")
  SCENARIO_NAME=$(yq e '.metadata.name' "$yaml_file")
  SCENARIO_DESCRIPTION=$(yq e '.metadata.description' "$yaml_file")
  SCENARIO_TAGS=$(yq e '.metadata.tags | join(", ")' "$yaml_file")

  # Validate version
  if [ "$SCENARIO_VERSION" != "2.0" ]; then
    log_warning "Unexpected scenario version: $SCENARIO_VERSION (expected 2.0)"
  fi

  # Validate required fields
  if [ "$SCENARIO_NAME" = "null" ] || [ -z "$SCENARIO_NAME" ]; then
    log_error "Scenario name is required (metadata.name)"
    exit 1
  fi

  log_debug "Scenario: $SCENARIO_NAME"
  log_debug "Tags: $SCENARIO_TAGS"
}

# Get user configuration
get_user_email() {
  # Check for command-line override first
  if [ -n "$OVERRIDE_USER_EMAIL" ]; then
    echo "$OVERRIDE_USER_EMAIL"
    return
  fi

  yq e '.setup.user.email' "$SCENARIO_FILE"
}

get_user_tier() {
  yq e '.setup.user.tier' "$SCENARIO_FILE"
}

get_user_mode() {
  # Check for command-line override first
  if [ -n "$OVERRIDE_USER_MODE" ]; then
    echo "$OVERRIDE_USER_MODE"
    return
  fi

  local mode=$(yq e '.setup.user.mode' "$SCENARIO_FILE")
  if [ "$mode" = "null" ] || [ -z "$mode" ]; then
    echo "new"  # Default to "new" for backward compatibility
  else
    echo "$mode"
  fi
}

get_user_clerk_id() {
  local clerk_id=$(yq e '.setup.user.clerk_user_id' "$SCENARIO_FILE")
  if [ "$clerk_id" = "null" ]; then
    echo ""
  else
    echo "$clerk_id"
  fi
}

# Get counts
get_linked_items_count() {
  yq e '.setup.linked_items | length' "$SCENARIO_FILE"
}

get_accounts_count() {
  yq e '.setup.accounts | length' "$SCENARIO_FILE"
}

get_assertions_count() {
  yq e '.assertions | length' "$SCENARIO_FILE"
}

# Get transaction mode
get_transaction_mode() {
  local count_mode=$(yq e '.setup.transactions.count_per_account' "$SCENARIO_FILE")
  local manual_count=$(yq e '.setup.transactions | length' "$SCENARIO_FILE")

  if [ "$count_mode" != "null" ]; then
    echo "auto"
  elif [ "$manual_count" -gt 0 ] && [ "$manual_count" != "null" ]; then
    echo "manual"
  else
    echo "none"
  fi
}

get_transaction_count_per_account() {
  yq e '.setup.transactions.count_per_account' "$SCENARIO_FILE"
}

get_manual_transactions_count() {
  local count=$(yq e '.setup.transactions | length' "$SCENARIO_FILE")
  if [ "$count" = "null" ]; then
    echo "0"
  else
    echo "$count"
  fi
}

# Get linked item by index
get_linked_item_institution() {
  local index=$1
  yq e ".setup.linked_items[$index].institution" "$SCENARIO_FILE"
}

# Get account by index
get_account_item_index() {
  local index=$1
  yq e ".setup.accounts[$index].item_index" "$SCENARIO_FILE"
}

get_account_type() {
  local index=$1
  yq e ".setup.accounts[$index].type" "$SCENARIO_FILE"
}

get_account_name() {
  local index=$1
  yq e ".setup.accounts[$index].name" "$SCENARIO_FILE"
}

get_account_balance() {
  local index=$1
  yq e ".setup.accounts[$index].balance" "$SCENARIO_FILE"
}

# Get manual transaction by index
get_transaction_account_index() {
  local index=$1
  yq e ".setup.transactions[$index].account_index" "$SCENARIO_FILE"
}

get_transaction_amount() {
  local index=$1
  yq e ".setup.transactions[$index].amount" "$SCENARIO_FILE"
}

get_transaction_date() {
  local index=$1
  yq e ".setup.transactions[$index].date" "$SCENARIO_FILE"
}

get_transaction_name() {
  local index=$1
  yq e ".setup.transactions[$index].name" "$SCENARIO_FILE"
}

get_transaction_pending() {
  local index=$1
  local pending=$(yq e ".setup.transactions[$index].pending" "$SCENARIO_FILE")
  if [ "$pending" = "null" ]; then
    echo "false"
  else
    echo "$pending"
  fi
}

get_transaction_user_notes() {
  local index=$1
  local notes=$(yq e ".setup.transactions[$index].user_notes" "$SCENARIO_FILE")
  if [ "$notes" = "null" ]; then
    echo ""
  else
    echo "$notes"
  fi
}

get_transaction_category() {
  local index=$1
  local category=$(yq e ".setup.transactions[$index].category[0]" "$SCENARIO_FILE")
  if [ "$category" = "null" ]; then
    echo "Shopping"
  else
    echo "$category"
  fi
}

# Get assertion by index
get_assertion_type() {
  local index=$1
  yq e ".assertions[$index].type" "$SCENARIO_FILE"
}

get_assertion_operator() {
  local index=$1
  local op=$(yq e ".assertions[$index].operator" "$SCENARIO_FILE")
  if [ "$op" = "null" ]; then
    echo "="
  else
    echo "$op"
  fi
}

get_assertion_value() {
  local index=$1
  yq e ".assertions[$index].value" "$SCENARIO_FILE"
}

get_assertion_expected() {
  local index=$1
  yq e ".assertions[$index].expected" "$SCENARIO_FILE"
}

# Get webhook configuration
get_webhooks_fire_after_setup() {
  local fire=$(yq e '.webhooks.fire_after_setup' "$SCENARIO_FILE")
  if [ "$fire" = "null" ] || [ "$fire" = "false" ]; then
    echo "false"
  else
    echo "true"
  fi
}

get_webhooks_code() {
  local code=$(yq e '.webhooks.webhook_code' "$SCENARIO_FILE")
  if [ "$code" = "null" ] || [ -z "$code" ]; then
    echo "DEFAULT_UPDATE"  # Default webhook code
  else
    echo "$code"
  fi
}

get_webhooks_wait_for_processing() {
  local wait=$(yq e '.webhooks.wait_for_processing' "$SCENARIO_FILE")
  if [ "$wait" = "null" ] || [ "$wait" = "false" ]; then
    echo "false"
  else
    echo "true"
  fi
}

get_webhooks_timeout() {
  local timeout=$(yq e '.webhooks.timeout_seconds' "$SCENARIO_FILE")
  if [ "$timeout" = "null" ] || [ -z "$timeout" ]; then
    echo "30"  # Default timeout
  else
    echo "$timeout"
  fi
}
