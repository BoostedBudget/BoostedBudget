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
source "$DIR/lib/db-operations.sh"

# Error trap
trap 'handle_error $? $LINENO $BASH_LINENO "$BASH_COMMAND"' ERR

# Usage message
usage() {
  cat << EOF
Usage: $(basename "$0") <scenario.yaml> [options]

Populate staging database with fake test data from YAML scenario.

Arguments:
  scenario.yaml          Path to YAML scenario file

Options:
  --no-cleanup           Skip cleanup of existing data (for debugging)
  --debug                Enable debug logging
  --existing-user EMAIL  Override mode to 'existing' and use this email
                         (Requires user to exist in database)

Examples:
  # Create fake user (mode: new)
  $(basename "$0") scenarios/01-demo-data-free-user.yaml

  # Use existing Auth0 user (mode: existing)
  $(basename "$0") scenarios/01-demo-data-free-user.yaml --existing-user you@example.com

  # Use scenario with mode: existing already set
  $(basename "$0") scenarios/99-existing-user-example.yaml

Environment:
  DATABASE_URL     Loaded from backend/.env
  DEBUG            Set to 'true' for debug logging
  AUTH_TOKEN       Auth0 JWT token for admin webhook firing (optional)
                   Required only if scenario has webhooks.fire_after_setup: true

For more information, see README.md
EOF
  exit 1
}

# Parse arguments
SCENARIO_FILE=""
NO_CLEANUP=false
EXISTING_USER_EMAIL=""

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
    --existing-user)
      EXISTING_USER_EMAIL="$2"
      shift 2
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
source "$DIR/lib/cleanup.sh"
source "$DIR/lib/setup-user.sh"
source "$DIR/lib/setup-items.sh"
source "$DIR/lib/setup-accounts.sh"
source "$DIR/lib/fake-data-generator.sh"
source "$DIR/lib/admin-webhook.sh"
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

# Apply command-line overrides
if [ -n "$EXISTING_USER_EMAIL" ]; then
  export OVERRIDE_USER_EMAIL="$EXISTING_USER_EMAIL"
  export OVERRIDE_USER_MODE="existing"
  log_info "Command-line override: --existing-user $EXISTING_USER_EMAIL (mode: existing)"
  echo ""
fi

# Cleanup phase (optional)
if [ "$NO_CLEANUP" = "false" ]; then
  USER_EMAIL=$(get_user_email)
  USER_MODE=$(get_user_mode)

  # Preserve user for existing mode, delete all for new mode
  PRESERVE_USER="false"
  if [ "$USER_MODE" = "existing" ]; then
    PRESERVE_USER="true"
  fi

  if ! cleanup_by_email "$USER_EMAIL" "$PRESERVE_USER"; then
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

# 5. Fire webhooks (optional - creates balance snapshots)
echo ""
FIRE_WEBHOOKS=$(get_webhooks_fire_after_setup)
if [ "$FIRE_WEBHOOKS" = "true" ]; then
  WEBHOOK_CODE=$(get_webhooks_code)
  WAIT_FOR_PROCESSING=$(get_webhooks_wait_for_processing)
  WEBHOOK_TIMEOUT=$(get_webhooks_timeout)

  # Check for AUTH_TOKEN environment variable
  if [ -z "${AUTH_TOKEN:-}" ]; then
    log_warning "AUTH_TOKEN not set. Skipping webhook firing."
    log_warning "Set AUTH_TOKEN environment variable to enable webhook integration."
  else
    if ! fire_webhooks_for_scenario "$WEBHOOK_CODE" "$AUTH_TOKEN" "$WAIT_FOR_PROCESSING" "$WEBHOOK_TIMEOUT"; then
      log_warning "Webhook firing completed with warnings. See above for details."
      # Don't fail the scenario - webhooks are optional
    fi
  fi
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
if command -v count_balance_snapshots_by_user_id &> /dev/null; then
  log_info "  Balance snapshots: $(count_balance_snapshots_by_user_id "$USER_ID")"
fi
echo ""

exit 0
