#!/bin/bash

# setup-user.sh - User creation functions
# Creates users based on YAML scenario configuration

# Global variables set by setup_user (bash 3.2+ compatible)
USER_ID=""
USER_EMAIL=""
USER_TIER=""
HISTORICAL_DAYS=""

# Create user from scenario
setup_user() {
  log_info "Setting up user..."

  # Parse user configuration from YAML
  USER_EMAIL=$(get_user_email)
  USER_TIER=$(get_user_tier)
  local user_mode=$(get_user_mode)
  local clerk_user_id=$(get_user_clerk_id)

  if [ "$USER_EMAIL" = "null" ] || [ -z "$USER_EMAIL" ]; then
    log_error "User email is required (setup.user.email)"
    return 1
  fi

  # Validate tier if specified
  if [ "$USER_TIER" != "null" ] && [ -n "$USER_TIER" ]; then
    case "$USER_TIER" in
      "free"|"premium_lite"|"premium"|"premium_plus")
        ;;
      *)
        log_error "Invalid tier: $USER_TIER (must be free, premium_lite, premium, or premium_plus)"
        return 1
        ;;
    esac
  fi

  # Handle based on mode
  if [ "$user_mode" = "existing" ]; then
    # ========== EXISTING USER MODE ==========
    log_info "Mode: existing (using real Clerk user)"

    # Lookup user by email
    USER_ID=$(get_user_id_by_email "$USER_EMAIL")

    if [ -z "$USER_ID" ]; then
      log_error "User not found: $USER_EMAIL"
      log_error ""
      log_error "To use mode: existing, the user must exist in the database."
      log_error "Please log into staging.boostedbudget.com via Clerk first to create the account."
      log_error ""
      log_error "Steps:"
      log_error "  1. Visit https://staging.boostedbudget.com"
      log_error "  2. Log in with: $USER_EMAIL"
      log_error "  3. Re-run this script"
      return 1
    fi

    # Optionally validate clerk_user_id if provided
    if [ -n "$clerk_user_id" ]; then
      local user_id_by_clerk=$(get_user_id_by_clerk_id "$clerk_user_id")
      if [ -z "$user_id_by_clerk" ]; then
        log_error "Clerk user ID not found: $clerk_user_id"
        return 1
      fi

      if [ "$user_id_by_clerk" != "$USER_ID" ]; then
        log_error "Clerk user ID mismatch!"
        log_error "  Email $USER_EMAIL belongs to user: $USER_ID"
        log_error "  But Clerk ID $clerk_user_id belongs to: $user_id_by_clerk"
        return 1
      fi

      log_debug "Clerk user ID validated: $clerk_user_id"
    fi

    # Get current tier if not specified
    if [ "$USER_TIER" = "null" ] || [ -z "$USER_TIER" ]; then
      USER_TIER=$(get_user_tier_by_id "$USER_ID")
      log_debug "Using existing tier: $USER_TIER"
    else
      # Update tier if specified and different
      local current_tier=$(get_user_tier_by_id "$USER_ID")
      if [ "$current_tier" != "$USER_TIER" ]; then
        log_info "Updating tier: $current_tier → $USER_TIER"
        if ! update_user_tier "$USER_ID" "$USER_TIER"; then
          log_error "Failed to update user tier"
          return 1
        fi
      else
        log_debug "Tier unchanged: $USER_TIER"
      fi
    fi

    # Get historical depth for tier
    HISTORICAL_DAYS=$(get_historical_days "$USER_TIER")

    log_success "Using existing user: $USER_EMAIL (tier: $USER_TIER, ${HISTORICAL_DAYS}-day history)"
    log_info "✅ Frontend will work - user can log in via Clerk to view data"

  else
    # ========== NEW USER MODE (FAKE DATA) ==========
    log_info "Mode: new (creating fake Clerk user)"
    log_warning "⚠️  Frontend will NOT work - fake Clerk users cannot log in"
    log_warning "    For frontend testing, use mode: existing with a real Clerk user"

    # Default tier if not specified
    if [ "$USER_TIER" = "null" ] || [ -z "$USER_TIER" ]; then
      log_warning "User tier not specified, defaulting to 'free'"
      USER_TIER="free"
    fi

    # Generate user ID
    USER_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')

    # Generate fake clerk user ID
    local fake_clerk_user_id="user_test$(uuidgen | cut -d'-' -f1)"

    # Get historical depth for tier
    HISTORICAL_DAYS=$(get_historical_days "$USER_TIER")

    log_debug "Creating user:"
    log_debug "  ID: $USER_ID"
    log_debug "  Email: $USER_EMAIL"
    log_debug "  Tier: $USER_TIER"
    log_debug "  Historical days: $HISTORICAL_DAYS"
    log_debug "  Clerk ID: $fake_clerk_user_id (fake)"

    # Insert user
    local sql="
      INSERT INTO users (
        id, clerk_user_id, email, subscription_tier, primary_region, created_at, updated_at
      )
      VALUES (
        '$USER_ID',
        '$fake_clerk_user_id',
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
