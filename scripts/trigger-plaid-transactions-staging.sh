#!/bin/bash
# BoostedBudget - Plaid Transaction Trigger Script for Staging
#
# This script helps trigger transaction syncing in the staging environment
# by using Plaid's sandbox testing APIs to fire webhooks and simulate transactions.
#
# Usage: ./scripts/trigger-plaid-transactions-staging.sh [command]
#
# Commands:
#   menu          - Interactive menu (default)
#   fire-webhook  - Fire a TRANSACTIONS webhook for all linked items
#   add-txn       - Simulate new transactions
#   check-status  - Check sync status
#   watch-logs    - Monitor backend logs for webhook processing

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Configuration
ENV_FILE="${PROJECT_DIR}/.env.staging"
DOCKER_COMPOSE_FILE="${PROJECT_DIR}/docker-compose.staging.yml"
POSTGRES_CONTAINER="boostedbudget-staging-postgres-1"
BACKEND_CONTAINER="boostedbudget-staging-backend-1"

#==============================================================================
# HELPER FUNCTIONS
#==============================================================================

# Print colored messages
print_header() {
    echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

# Load environment variables
load_env() {
    if [ ! -f "$ENV_FILE" ]; then
        print_error "Environment file not found: $ENV_FILE"
        exit 1
    fi

    # Export variables from .env.staging
    export $(grep -v '^#' "$ENV_FILE" | grep -v '^$' | xargs)

    # Validate required variables
    if [ -z "$PLAID_CLIENT_ID" ] || [ -z "$PLAID_SECRET" ]; then
        print_error "Missing required Plaid credentials in $ENV_FILE"
        exit 1
    fi

    print_success "Loaded environment from $ENV_FILE"
    print_info "Plaid Client ID: $PLAID_CLIENT_ID"
    print_info "Plaid Environment: ${PLAID_ENV:-sandbox}"
}

# Check if Docker containers are running
check_containers() {
    if ! docker ps | grep -q "$POSTGRES_CONTAINER"; then
        print_error "PostgreSQL container is not running: $POSTGRES_CONTAINER"
        print_info "Start staging environment with: docker-compose -f docker-compose.staging.yml up -d"
        exit 1
    fi

    if ! docker ps | grep -q "$BACKEND_CONTAINER"; then
        print_warning "Backend container is not running: $BACKEND_CONTAINER"
        print_info "Webhooks won't be processed without the backend running"
    fi

    print_success "Docker containers are running"
}

# Query database for linked items
get_linked_items() {
    local query="SELECT id, institution_name, user_id FROM linked_items ORDER BY created_at DESC;"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -t -A -F'|' -c "$query" 2>/dev/null
}

# Count linked items
count_linked_items() {
    local query="SELECT COUNT(*) FROM linked_items;"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -t -A -c "$query" 2>/dev/null
}

# Get access token for a linked item (encrypted in DB)
get_access_token() {
    local item_id="$1"
    local query="SELECT access_token_encrypted FROM linked_items WHERE id = '$item_id';"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -t -A -c "$query" 2>/dev/null
}

# Count transactions
count_transactions() {
    local query="SELECT COUNT(*) FROM transactions;"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -t -A -c "$query" 2>/dev/null
}

# Count webhook events
count_webhooks() {
    local query="SELECT COUNT(*) FROM webhook_events;"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -t -A -c "$query" 2>/dev/null
}

# Get recent webhook events
get_recent_webhooks() {
    local limit="${1:-5}"
    local query="SELECT webhook_type, webhook_code, processed, created_at FROM webhook_events ORDER BY created_at DESC LIMIT $limit;"

    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -c "$query" 2>/dev/null
}

#==============================================================================
# PLAID API FUNCTIONS
#==============================================================================

# Fire a Plaid webhook
fire_plaid_webhook() {
    local access_token="$1"
    local webhook_code="${2:-DEFAULT_UPDATE}"

    print_info "Firing Plaid webhook: $webhook_code"

    local response=$(curl -s -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \
        -H 'Content-Type: application/json' \
        -d "{
            \"client_id\": \"$PLAID_CLIENT_ID\",
            \"secret\": \"$PLAID_SECRET\",
            \"access_token\": \"$access_token\",
            \"webhook_code\": \"$webhook_code\"
        }")

    if echo "$response" | grep -q "error_code"; then
        print_error "Failed to fire webhook"
        echo "$response" | jq '.' 2>/dev/null || echo "$response"
        return 1
    else
        print_success "Webhook fired successfully"
        echo "$response" | jq '.' 2>/dev/null || echo "$response"
        return 0
    fi
}

# Simulate new transactions
simulate_transactions() {
    local access_token="$1"
    local transaction_type="${2:-digital}"

    print_info "Simulating transactions of type: $transaction_type"

    local response=$(curl -s -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \
        -H 'Content-Type: application/json' \
        -d "{
            \"client_id\": \"$PLAID_CLIENT_ID\",
            \"secret\": \"$PLAID_SECRET\",
            \"access_token\": \"$access_token\",
            \"webhook_code\": \"DEFAULT_UPDATE\"
        }")

    if echo "$response" | grep -q "error_code"; then
        print_error "Failed to simulate transactions"
        echo "$response" | jq '.' 2>/dev/null || echo "$response"
        return 1
    else
        print_success "Transactions simulated successfully"
        print_info "This will trigger a DEFAULT_UPDATE webhook"
        return 0
    fi
}

# Get item info via Plaid API
get_item_info() {
    local access_token="$1"

    local response=$(curl -s -X POST https://sandbox.plaid.com/item/get \
        -H 'Content-Type: application/json' \
        -d "{
            \"client_id\": \"$PLAID_CLIENT_ID\",
            \"secret\": \"$PLAID_SECRET\",
            \"access_token\": \"$access_token\"
        }")

    echo "$response" | jq '.' 2>/dev/null || echo "$response"
}

#==============================================================================
# MAIN COMMANDS
#==============================================================================

# Command: Fire webhooks for all linked items
cmd_fire_webhook() {
    print_header "Fire Plaid Webhooks"

    local count=$(count_linked_items)

    if [ "$count" -eq 0 ]; then
        print_warning "No linked items found in database"
        print_info "Link a bank account through the frontend first"
        return 1
    fi

    print_info "Found $count linked item(s)"
    echo ""

    # Note: Access tokens are encrypted in the database
    # We can't decrypt them from this script without the encryption key
    # Instead, we'll provide instructions for manual webhook firing

    print_warning "Access tokens are encrypted in the database"
    print_info "To fire webhooks manually, you have two options:"
    echo ""
    echo "Option 1: Use the backend API (recommended)"
    echo "  - The backend can decrypt access tokens and fire webhooks"
    echo "  - TODO: Create a /api/v1/admin/fire-webhook endpoint"
    echo ""
    echo "Option 2: Fire webhooks via Plaid Dashboard"
    echo "  1. Go to https://dashboard.plaid.com/activity/events"
    echo "  2. Select your sandbox environment"
    echo "  3. Find your item and click 'Send test webhook'"
    echo ""
    echo "Option 3: Get access token from backend logs"
    echo "  - When you link an account, the access token is logged (temporarily)"
    echo "  - Search backend logs for 'Exchanged public token'"
    echo ""

    print_info "Showing linked items:"
    get_linked_items | while IFS='|' read -r id institution user_id; do
        echo "  - Item ID: $id"
        echo "    Institution: $institution"
        echo "    User ID: $user_id"
        echo ""
    done

    print_info "To fire a webhook manually with curl:"
    echo ""
    echo "curl -X POST https://sandbox.plaid.com/sandbox/item/fire_webhook \\"
    echo "  -H 'Content-Type: application/json' \\"
    echo "  -d '{"
    echo "    \"client_id\": \"$PLAID_CLIENT_ID\","
    echo "    \"secret\": \"$PLAID_SECRET\","
    echo "    \"access_token\": \"YOUR_ACCESS_TOKEN\","
    echo "    \"webhook_code\": \"DEFAULT_UPDATE\""
    echo "  }'"
    echo ""

    print_info "Available webhook codes:"
    echo "  - INITIAL_UPDATE: Historical transactions ready"
    echo "  - DEFAULT_UPDATE: New transactions available"
    echo "  - HISTORICAL_UPDATE: Historical data updated"
    echo "  - TRANSACTIONS_REMOVED: Transactions removed"
}

# Command: Check sync status
cmd_check_status() {
    print_header "Transaction Sync Status"

    local linked_count=$(count_linked_items)
    local txn_count=$(count_transactions)
    local webhook_count=$(count_webhooks)

    echo "Linked Items:     $linked_count"
    echo "Transactions:     $txn_count"
    echo "Webhook Events:   $webhook_count"
    echo ""

    if [ "$webhook_count" -gt 0 ]; then
        print_info "Recent webhooks:"
        get_recent_webhooks 5
    else
        print_warning "No webhooks received yet"
        print_info "Your webhook URL may not be reachable from Plaid"
        print_info "Current webhook URL: $PLAID_WEBHOOK_URL"
        echo ""
        print_info "For staging/local testing, use the fire_webhook command instead"
    fi

    echo ""

    if [ "$txn_count" -eq 0 ] && [ "$linked_count" -gt 0 ]; then
        print_warning "You have linked items but no transactions"
        print_info "This suggests webhooks aren't being delivered or processed"
        echo ""
        print_info "Troubleshooting steps:"
        echo "  1. Fire a manual webhook using the 'fire-webhook' command"
        echo "  2. Check backend logs with 'watch-logs' command"
        echo "  3. Verify webhook processor is running in backend logs"
    fi
}

# Command: Watch backend logs
cmd_watch_logs() {
    print_header "Watching Backend Logs"

    print_info "Filtering for webhook, plaid, and transaction events"
    print_info "Press Ctrl+C to stop"
    echo ""

    docker logs -f "$BACKEND_CONTAINER" 2>&1 | grep --line-buffered -i "webhook\|plaid\|transaction\|sync"
}

# Command: Show database info
cmd_db_info() {
    print_header "Database Information"

    print_info "Linked Items:"
    get_linked_items | while IFS='|' read -r id institution user_id; do
        echo "  - $institution (ID: $id)"
    done
    echo ""

    print_info "Recent transactions (top 10):"
    local query="SELECT t.name, t.amount, t.date, a.name as account FROM transactions t JOIN accounts a ON t.account_id = a.id ORDER BY t.created_at DESC LIMIT 10;"
    docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d boostedbudget_staging -c "$query" 2>/dev/null
}

# Command: Interactive menu
cmd_menu() {
    print_header "Plaid Transaction Testing - Staging"

    echo "Choose an option:"
    echo ""
    echo "  1) Check sync status"
    echo "  2) Fire webhook (manual instructions)"
    echo "  3) Watch backend logs"
    echo "  4) Show database info"
    echo "  5) Exit"
    echo ""
    read -p "Enter choice [1-5]: " choice

    case $choice in
        1) cmd_check_status ;;
        2) cmd_fire_webhook ;;
        3) cmd_watch_logs ;;
        4) cmd_db_info ;;
        5) exit 0 ;;
        *) print_error "Invalid choice"; exit 1 ;;
    esac
}

#==============================================================================
# MAIN
#==============================================================================

main() {
    # Load environment and check prerequisites
    load_env
    check_containers

    # Parse command
    local command="${1:-menu}"

    case $command in
        menu)
            cmd_menu
            ;;
        fire-webhook)
            cmd_fire_webhook
            ;;
        check-status)
            cmd_check_status
            ;;
        watch-logs)
            cmd_watch_logs
            ;;
        db-info)
            cmd_db_info
            ;;
        help|--help|-h)
            echo "Usage: $0 [command]"
            echo ""
            echo "Commands:"
            echo "  menu           Interactive menu (default)"
            echo "  fire-webhook   Fire TRANSACTIONS webhook"
            echo "  check-status   Check sync status"
            echo "  watch-logs     Monitor backend logs"
            echo "  db-info        Show database info"
            echo "  help           Show this help"
            ;;
        *)
            print_error "Unknown command: $command"
            echo "Run '$0 help' for usage information"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
