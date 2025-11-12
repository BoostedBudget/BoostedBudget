#!/bin/bash

# setup-user.sh - User creation functions
# Creates users based on YAML scenario configuration

# Global variable set by setup_user
declare -g USER_ID
declare -g USER_EMAIL
declare -g USER_TIER
declare -g HISTORICAL_DAYS

# Create user from scenario
setup_user() {
  log_info "Setting up user..."

  # Parse user configuration from YAML
  USER_EMAIL=$(get_user_email)
  USER_TIER=$(get_user_tier)

  if [ "$USER_EMAIL" = "null" ] || [ -z "$USER_EMAIL" ]; then
    log_error "User email is required (setup.user.email)"
    return 1
  fi

  if [ "$USER_TIER" = "null" ] || [ -z "$USER_TIER" ]; then
    log_warning "User tier not specified, defaulting to 'free'"
    USER_TIER="free"
  fi

  # Validate tier
  case "$USER_TIER" in
    "free"|"premium_lite"|"premium"|"premium_plus")
      ;;
    *)
      log_error "Invalid tier: $USER_TIER (must be free, premium_lite, premium, or premium_plus)"
      return 1
      ;;
  esac

  # Generate user ID
  USER_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')

  # Generate auth0 user ID
  local auth0_user_id="auth0|test-$(uuidgen | cut -d'-' -f1)"

  # Get historical depth for tier
  HISTORICAL_DAYS=$(get_historical_days "$USER_TIER")

  log_debug "Creating user:"
  log_debug "  ID: $USER_ID"
  log_debug "  Email: $USER_EMAIL"
  log_debug "  Tier: $USER_TIER"
  log_debug "  Historical days: $HISTORICAL_DAYS"

  # Insert user
  local sql="
    INSERT INTO users (
      id, auth0_user_id, email, subscription_tier, primary_region, created_at, updated_at
    )
    VALUES (
      '$USER_ID',
      '$auth0_user_id',
      '$USER_EMAIL',
      '$USER_TIER',
      'us-east-1',
      NOW(),
      NOW()
    );
  "

  if execute_db "$sql"; then
    log_success "User created: $USER_EMAIL (tier: $USER_TIER, ${HISTORICAL_DAYS}-day history)"
    return 0
  else
    log_error "Failed to create user"
    return 1
  fi
}

# Verify user was created
verify_user_created() {
  if [ -z "$USER_ID" ]; then
    log_error "USER_ID not set"
    return 1
  fi

  local count
  count=$(query_db "SELECT COUNT(*) FROM users WHERE id = '$USER_ID';")

  if [ "$count" -eq 1 ]; then
    log_debug "User verified: $USER_ID"
    return 0
  else
    log_error "User verification failed: $USER_ID"
    return 1
  fi
}
