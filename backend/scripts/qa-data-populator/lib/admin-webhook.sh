#!/bin/bash

# admin-webhook.sh
# Library for firing admin webhooks to trigger balance snapshot creation
# Issue #353: Balance Tracking Dashboard - Staging Test Support

set -euo pipefail

# Source dependencies
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/logging.sh"
source "$SCRIPT_DIR/db-operations.sh"

# Admin webhook endpoint URL (from environment or default to staging)
ADMIN_WEBHOOK_URL="${ADMIN_WEBHOOK_URL:-https://staging.boostedbudget.com/api/v1/admin/plaid/fire-webhook}"

# Fire admin webhook for linked item
# Args:
#   $1 - item_id (Plaid item ID, not UUID)
#   $2 - webhook_code (DEFAULT_UPDATE, INITIAL_UPDATE, HISTORICAL_UPDATE, TRANSACTIONS_REMOVED)
#   $3 - auth_token (Auth0 JWT token)
# Returns:
#   0 on success, 1 on failure
fire_admin_webhook() {
  local item_id=$1
  local webhook_code=$2
  local auth_token=$3

  log_info "Firing webhook: $webhook_code for item $item_id"

  # Validate webhook code
  case "$webhook_code" in
    DEFAULT_UPDATE|INITIAL_UPDATE|HISTORICAL_UPDATE|TRANSACTIONS_REMOVED)
      ;;
    *)
      log_error "Invalid webhook code: $webhook_code"
      log_error "Valid codes: DEFAULT_UPDATE, INITIAL_UPDATE, HISTORICAL_UPDATE, TRANSACTIONS_REMOVED"
      return 1
      ;;
  esac

  # Make API request
  local response
  local http_code

  response=$(curl -s -w "\n%{http_code}" -X POST "$ADMIN_WEBHOOK_URL" \
    -H "Authorization: Bearer $auth_token" \
    -H "Content-Type: application/json" \
    -d "{\"item_id\":\"$item_id\",\"webhook_code\":\"$webhook_code\"}" \
    2>&1)

  http_code=$(echo "$response" | tail -n 1)
  body=$(echo "$response" | sed '$d')

  # Check HTTP status code
  if [ "$http_code" -eq 200 ] || [ "$http_code" -eq 201 ]; then
    log_success "Webhook fired successfully: $webhook_code for item $item_id"
    log_debug "Response: $body"
    return 0
  else
    log_error "Failed to fire webhook. HTTP $http_code"
    log_error "Response: $body"
    return 1
  fi
}

# Wait for webhook to be processed
# Args:
#   $1 - item_id (Plaid item ID)
#   $2 - timeout_seconds (optional, default: 30)
# Returns:
#   0 if webhook processed, 1 on timeout or error
wait_for_webhook_processed() {
  local item_id=$1
  local timeout=${2:-30}
  local elapsed=0
  local poll_interval=2

  log_info "Waiting for webhook to be processed (timeout: ${timeout}s)..."

  while [ $elapsed -lt $timeout ]; do
    # Query for most recent processed webhook for this item
    local processed_count
    processed_count=$(query_db "SELECT COUNT(*) FROM webhook_events WHERE item_id = '$item_id' AND processed = true ORDER BY created_at DESC LIMIT 1;")

    if [ "$processed_count" -gt 0 ]; then
      log_success "Webhook processed successfully for item $item_id (elapsed: ${elapsed}s)"
      return 0
    fi

    # Show progress indicator
    if [ $((elapsed % 10)) -eq 0 ] && [ $elapsed -gt 0 ]; then
      log_debug "Still waiting... (${elapsed}s/${timeout}s)"
    fi

    sleep $poll_interval
    elapsed=$((elapsed + poll_interval))
  done

  log_error "Webhook processing timeout after ${timeout}s for item $item_id"
  log_error "Webhook may still be processing. Check webhook_events table."
  return 1
}

# Fire webhooks for all linked items in the scenario
# Args:
#   $1 - webhook_code
#   $2 - auth_token
#   $3 - wait_for_processing (true/false)
#   $4 - timeout_seconds (optional)
# Returns:
#   0 if all webhooks fired successfully, 1 on any failure
fire_webhooks_for_scenario() {
  local webhook_code=$1
  local auth_token=$2
  local wait_for_processing=$3
  local timeout=${4:-30}

  log_section "FIRING WEBHOOKS"

  if [ -z "$auth_token" ]; then
    log_error "AUTH_TOKEN not provided. Cannot fire admin webhooks."
    log_error "Set AUTH_TOKEN environment variable with valid Auth0 JWT."
    return 1
  fi

  # Get linked item IDs from database
  local item_ids
  item_ids=$(query_db "SELECT item_id FROM linked_items WHERE user_id = '$USER_ID';" false)

  if [ -z "$item_ids" ]; then
    log_warning "No linked items found for user $USER_ID"
    return 0
  fi

  local total_items
  total_items=$(echo "$item_ids" | wc -l | tr -d ' ')
  log_info "Found $total_items linked item(s) to process"

  local success_count=0
  local failure_count=0

  # Process each linked item
  while IFS= read -r item_id; do
    if [ -z "$item_id" ]; then
      continue
    fi

    log_info "Processing item: $item_id"

    # Fire webhook
    if fire_admin_webhook "$item_id" "$webhook_code" "$auth_token"; then
      success_count=$((success_count + 1))

      # Wait for processing if requested
      if [ "$wait_for_processing" = "true" ]; then
        if ! wait_for_webhook_processed "$item_id" "$timeout"; then
          log_warning "Webhook timeout for item $item_id, but continuing..."
          # Don't fail the entire scenario on timeout
        fi
      fi
    else
      failure_count=$((failure_count + 1))
      log_warning "Webhook failed for item $item_id, but continuing..."
      # Don't fail the entire scenario on individual webhook failure
    fi
  done <<< "$item_ids"

  # Summary
  log_info "Webhook firing complete: $success_count succeeded, $failure_count failed"

  if [ $failure_count -gt 0 ]; then
    log_warning "Some webhooks failed. Check logs above for details."
    return 1
  fi

  return 0
}

# Get webhook processing status for item
# Args:
#   $1 - item_id (Plaid item ID)
# Returns:
#   0 if processed, 1 if pending/failed
get_webhook_status() {
  local item_id=$1

  local status
  status=$(query_db "
    SELECT
      CASE
        WHEN processed = true THEN 'processed'
        WHEN processing_started_at IS NOT NULL THEN 'processing'
        ELSE 'pending'
      END as status
    FROM webhook_events
    WHERE item_id = '$item_id'
    ORDER BY created_at DESC
    LIMIT 1;
  ")

  echo "$status"
}

# Verify balance snapshots were created after webhook
# Args:
#   $1 - user_id
#   $2 - expected_count (optional, for validation)
# Returns:
#   0 if snapshots exist, 1 otherwise
verify_balance_snapshots_created() {
  local user_id=$1
  local expected_count=${2:-1}

  log_info "Verifying balance snapshots were created..."

  local actual_count
  actual_count=$(query_db "SELECT COUNT(*) FROM balance_snapshots WHERE user_id = '$user_id' AND DATE(snapshot_at) = CURRENT_DATE;")

  if [ "$actual_count" -ge "$expected_count" ]; then
    log_success "Balance snapshots created: $actual_count snapshot(s) for today"
    return 0
  else
    log_error "Balance snapshots missing. Expected >= $expected_count, got $actual_count"
    return 1
  fi
}

# Export functions for use in populate.sh
export -f fire_admin_webhook
export -f wait_for_webhook_processed
export -f fire_webhooks_for_scenario
export -f get_webhook_status
export -f verify_balance_snapshots_created
