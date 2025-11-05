#!/bin/bash
# API Client Library for BoostedBudget Admin Webhook Testing
# Provides functions to interact with the admin webhook endpoint

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Validate required environment variables
validate_api_env() {
    local missing=()

    [[ -z "${API_BASE_URL:-}" ]] && missing+=("API_BASE_URL")
    [[ -z "${AUTH_TOKEN:-}" ]] && missing+=("AUTH_TOKEN")

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing required environment variables: ${missing[*]}"
        log_error "Please set these in your config file or environment"
        return 1
    fi

    log_info "API environment validated: $API_BASE_URL"
    return 0
}

# Fire a Plaid webhook via the admin API
# Usage: fire_webhook <item_id> <webhook_code> [auth_token]
# Returns: JSON response from API
fire_webhook() {
    local item_id="$1"
    local webhook_code="$2"
    local auth_token="${3:-$AUTH_TOKEN}"

    if [[ -z "$item_id" ]]; then
        log_error "fire_webhook: item_id is required"
        return 1
    fi

    if [[ -z "$webhook_code" ]]; then
        log_error "fire_webhook: webhook_code is required"
        return 1
    fi

    # Validate webhook code
    case "$webhook_code" in
        INITIAL_UPDATE|DEFAULT_UPDATE|HISTORICAL_UPDATE|TRANSACTIONS_REMOVED)
            ;;
        *)
            log_error "Invalid webhook_code: $webhook_code"
            log_error "Valid codes: INITIAL_UPDATE, DEFAULT_UPDATE, HISTORICAL_UPDATE, TRANSACTIONS_REMOVED"
            return 1
            ;;
    esac

    log_info "Firing webhook: item_id=$item_id, code=$webhook_code"

    local payload
    payload=$(cat <<EOF
{
  "item_id": "$item_id",
  "webhook_code": "$webhook_code"
}
EOF
    )

    local response
    local http_code

    # Make API call and capture response + HTTP code
    response=$(curl -s -w "\n%{http_code}" -X POST \
        "${API_BASE_URL}/api/v1/admin/plaid/fire-webhook" \
        -H "Authorization: Bearer $auth_token" \
        -H "Content-Type: application/json" \
        -d "$payload")

    # Split response and HTTP code
    http_code=$(echo "$response" | tail -n1)
    response_body=$(echo "$response" | sed '$d')

    # Check HTTP status
    if [[ "$http_code" != "200" ]]; then
        log_error "Webhook firing failed with HTTP $http_code"
        log_error "Response: $response_body"
        return 1
    fi

    # Check if webhook_fired is true
    local webhook_fired
    webhook_fired=$(echo "$response_body" | jq -r '.webhook_fired // false')

    if [[ "$webhook_fired" == "true" ]]; then
        local request_id
        request_id=$(echo "$response_body" | jq -r '.request_id // "unknown"')
        log_success "Webhook fired successfully (request_id: $request_id)"
        echo "$response_body"
        return 0
    else
        log_error "Webhook firing failed: webhook_fired=false"
        log_error "Response: $response_body"
        return 1
    fi
}

# Wait for webhook to be processed in the database
# Usage: wait_for_webhook_processed <item_id> [timeout_seconds]
# Returns: 0 if processed, 1 if timeout
wait_for_webhook_processed() {
    local item_id="$1"
    local timeout="${2:-30}"
    local start_time
    start_time=$(date +%s)

    if [[ -z "$item_id" ]]; then
        log_error "wait_for_webhook_processed: item_id is required"
        return 1
    fi

    log_info "Waiting for webhook to be processed (timeout: ${timeout}s)..."

    local elapsed=0
    while true; do
        elapsed=$(($(date +%s) - start_time))

        if [[ $elapsed -gt $timeout ]]; then
            log_error "Timeout waiting for webhook processing after ${timeout}s"
            return 1
        fi

        # Query database for most recent webhook with this item_id
        local processed
        processed=$(psql "${DATABASE_URL}" -t -A -c \
            "SELECT processed FROM webhook_events WHERE item_id = '$item_id' ORDER BY created_at DESC LIMIT 1" \
            2>/dev/null)

        if [[ $? -ne 0 ]]; then
            log_error "Database query failed"
            return 1
        fi

        if [[ "$processed" == "t" ]]; then
            log_success "Webhook processed (elapsed: ${elapsed}s)"
            return 0
        fi

        # Show progress
        if [[ $((elapsed % 5)) -eq 0 ]] && [[ $elapsed -gt 0 ]]; then
            log_info "Still waiting... (${elapsed}s / ${timeout}s)"
        fi

        sleep 1
    done
}

# Get webhook processing status
# Usage: get_webhook_status <item_id>
# Returns: JSON object with webhook status
get_webhook_status() {
    local item_id="$1"

    if [[ -z "$item_id" ]]; then
        log_error "get_webhook_status: item_id is required"
        return 1
    fi

    local result
    result=$(psql "${DATABASE_URL}" -t -A -F'|' -c \
        "SELECT
            webhook_type,
            webhook_code,
            processed,
            processing_error,
            retry_count,
            created_at
        FROM webhook_events
        WHERE item_id = '$item_id'
        ORDER BY created_at DESC
        LIMIT 1" \
        2>/dev/null)

    if [[ $? -ne 0 ]]; then
        log_error "Database query failed"
        return 1
    fi

    if [[ -z "$result" ]]; then
        log_warning "No webhook found for item_id: $item_id"
        return 1
    fi

    # Parse result
    IFS='|' read -r webhook_type webhook_code processed processing_error retry_count created_at <<< "$result"

    # Build JSON response
    cat <<EOF
{
  "webhook_type": "$webhook_type",
  "webhook_code": "$webhook_code",
  "processed": $([[ "$processed" == "t" ]] && echo "true" || echo "false"),
  "processing_error": $([ -n "$processing_error" ] && echo "\"$processing_error\"" || echo "null"),
  "retry_count": ${retry_count:-0},
  "created_at": "$created_at"
}
EOF
}

# Health check for the admin API
# Usage: check_admin_api_health
# Returns: 0 if healthy, 1 if not
check_admin_api_health() {
    log_info "Checking admin API health..."

    local response
    local http_code

    response=$(curl -s -w "\n%{http_code}" -X GET "${API_BASE_URL}/health" 2>/dev/null)

    http_code=$(echo "$response" | tail -n1)
    response_body=$(echo "$response" | sed '$d')

    if [[ "$http_code" == "200" ]]; then
        local status
        status=$(echo "$response_body" | jq -r '.status // "unknown"')
        log_success "Admin API is healthy (status: $status)"
        return 0
    else
        log_error "Admin API health check failed (HTTP $http_code)"
        return 1
    fi
}

# Test authentication token
# Usage: test_auth_token [auth_token]
# Returns: 0 if valid, 1 if invalid
test_auth_token() {
    local auth_token="${1:-$AUTH_TOKEN}"

    if [[ -z "$auth_token" ]]; then
        log_error "test_auth_token: auth_token is required"
        return 1
    fi

    log_info "Testing authentication token..."

    # Try to call a protected endpoint (using a dummy request that will fail validation but test auth)
    local response
    local http_code

    response=$(curl -s -w "\n%{http_code}" -X POST \
        "${API_BASE_URL}/api/v1/admin/plaid/fire-webhook" \
        -H "Authorization: Bearer $auth_token" \
        -H "Content-Type: application/json" \
        -d '{"item_id":"test","webhook_code":"INVALID"}' \
        2>/dev/null)

    http_code=$(echo "$response" | tail -n1)

    # 400 = auth worked, validation failed (expected)
    # 401 = auth failed
    # 404 = endpoint not available (production)
    if [[ "$http_code" == "400" ]]; then
        log_success "Authentication token is valid"
        return 0
    elif [[ "$http_code" == "401" ]]; then
        log_error "Authentication token is invalid or expired"
        return 1
    elif [[ "$http_code" == "404" ]]; then
        log_error "Admin endpoint not available (are you in staging environment?)"
        return 1
    else
        log_warning "Unexpected HTTP code: $http_code (might still be valid)"
        return 0
    fi
}

# Batch fire webhooks for multiple items
# Usage: batch_fire_webhooks <webhook_code> <item_id_1> <item_id_2> ...
# Returns: Number of successful webhook fires
batch_fire_webhooks() {
    local webhook_code="$1"
    shift
    local item_ids=("$@")

    if [[ -z "$webhook_code" ]]; then
        log_error "batch_fire_webhooks: webhook_code is required"
        return 1
    fi

    if [[ ${#item_ids[@]} -eq 0 ]]; then
        log_error "batch_fire_webhooks: at least one item_id is required"
        return 1
    fi

    log_info "Batch firing $webhook_code webhook for ${#item_ids[@]} items..."

    local success_count=0
    local fail_count=0

    for item_id in "${item_ids[@]}"; do
        if fire_webhook "$item_id" "$webhook_code" >/dev/null 2>&1; then
            ((success_count++))
        else
            ((fail_count++))
            log_warning "Failed to fire webhook for item: $item_id"
        fi

        # Small delay between requests to avoid rate limiting
        sleep 0.5
    done

    log_info "Batch complete: $success_count succeeded, $fail_count failed"
    return $success_count
}

# Export functions for use in other scripts
export -f validate_api_env
export -f fire_webhook
export -f wait_for_webhook_processed
export -f get_webhook_status
export -f check_admin_api_health
export -f test_auth_token
export -f batch_fire_webhooks
export -f log_info
export -f log_success
export -f log_error
export -f log_warning
