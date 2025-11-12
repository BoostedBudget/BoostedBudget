#!/bin/bash

# yaml-parser.sh - YAML parsing utilities using yq
# Provides helper functions for reading scenario YAML files

# Global variables set by parse_scenario
declare -g SCENARIO_VERSION
declare -g SCENARIO_NAME
declare -g SCENARIO_DESCRIPTION
declare -g SCENARIO_TAGS

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
  yq e '.setup.user.email' "$SCENARIO_FILE"
}

get_user_tier() {
  yq e '.setup.user.tier' "$SCENARIO_FILE"
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
