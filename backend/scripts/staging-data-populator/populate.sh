#!/bin/bash

# populate.sh - Main orchestrator for staging data population
# Entry point for running YAML scenarios

set -euo pipefail

# Script directory
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Start timer
START_TIME=$(date +%s)

# Load dependencies
source "$DIR/lib/logging.sh"
source "$DIR/lib/config.sh"

# Error trap
trap 'handle_error $? $LINENO $BASH_LINENO "$BASH_COMMAND"' ERR

# Usage message
usage() {
  cat << EOF
Usage: $(basename "$0") <scenario.yaml>

Populate staging database with fake test data from YAML scenario.

Arguments:
  scenario.yaml    Path to YAML scenario file

Examples:
  $(basename "$0") scenarios/01-demo-data-free-user.yaml
  $(basename "$0") scenarios/02-demo-data-premium-user.yaml

Options:
  --no-cleanup     Skip cleanup of existing data (for debugging)
  --debug          Enable debug logging

Environment:
  DATABASE_URL     Loaded from backend/.env
  DEBUG            Set to 'true' for debug logging

For more information, see README.md
EOF
  exit 1
}

# Parse arguments
SCENARIO_FILE=""
NO_CLEANUP=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --no-cleanup)
      NO_CLEANUP=true
      shift
      ;;
    --debug)
      export DEBUG=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      if [ -z "$SCENARIO_FILE" ]; then
        SCENARIO_FILE="$1"
      else
        log_error "Unknown argument: $1"
        usage
      fi
      shift
      ;;
  esac
done

# Validate scenario file
if [ -z "$SCENARIO_FILE" ]; then
  log_error "Scenario file is required"
  usage
fi

if [ ! -f "$SCENARIO_FILE" ]; then
  log_error "Scenario file not found: $SCENARIO_FILE"
  exit 1
fi

# Initialize
log_info "🚀 Staging Data Populator"
echo ""

init_config

# Validate database
if ! validate_db_connection; then
  exit 1
fi

if ! validate_database_schema; then
  exit 1
fi

# Load remaining dependencies
source "$DIR/lib/yaml-parser.sh"
source "$DIR/lib/db-operations.sh"
source "$DIR/lib/cleanup.sh"
source "$DIR/lib/setup-user.sh"
source "$DIR/lib/setup-items.sh"
source "$DIR/lib/setup-accounts.sh"
source "$DIR/lib/fake-data-generator.sh"
source "$DIR/lib/assertions.sh"

# Parse scenario
parse_scenario "$SCENARIO_FILE"

log_info "Scenario: $SCENARIO_NAME"
if [ -n "$SCENARIO_DESCRIPTION" ] && [ "$SCENARIO_DESCRIPTION" != "null" ]; then
  log_info "Description: $SCENARIO_DESCRIPTION"
fi
if [ -n "$SCENARIO_TAGS" ] && [ "$SCENARIO_TAGS" != "null" ]; then
  log_info "Tags: $SCENARIO_TAGS"
fi
echo ""

# Cleanup phase (optional)
if [ "$NO_CLEANUP" = "false" ]; then
  USER_EMAIL=$(get_user_email)
  if ! cleanup_by_email "$USER_EMAIL"; then
    log_error "Cleanup failed"
    exit 1
  fi
  echo ""
fi

# Setup phase
log_section "SETUP PHASE"

# 1. Create user
if ! setup_user; then
  log_error "User setup failed"
  exit 1
fi

# 2. Create linked items
if ! setup_linked_items; then
  log_error "Linked items setup failed"
  exit 1
fi

# 3. Create accounts
if ! setup_accounts; then
  log_error "Accounts setup failed"
  exit 1
fi

# 4. Generate transactions
echo ""
if ! generate_transactions; then
  log_error "Transaction generation failed"
  exit 1
fi

# Assertion phase
echo ""
if ! run_assertions; then
  log_error "Assertions failed"
  exit 1
fi

# Summary
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

log_summary "success" "$DURATION"

echo ""
log_info "Results:"
log_info "  User: $USER_EMAIL"
log_info "  Tier: $USER_TIER"
log_info "  Accounts: $(count_accounts_by_user_id "$USER_ID")"
log_info "  Transactions: $(count_transactions_by_user_id "$USER_ID")"
log_info "  Historical days: $HISTORICAL_DAYS"
echo ""

exit 0
