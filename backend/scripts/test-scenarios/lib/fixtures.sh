#!/bin/bash
# Fixtures Library for BoostedBudget Test Scenarios
# Provides data generation functions for creating realistic test data

# Source logging functions if not already loaded
if ! type -t log_info &>/dev/null; then
    source "$(dirname "${BASH_SOURCE[0]}")/api-client.sh"
fi

# ============================================================================
# ID Generators
# ============================================================================

# Generate UUID v4
# Usage: generate_uuid
generate_uuid() {
    # Use uuidgen if available, otherwise use /proc/sys/kernel/random/uuid
    if command -v uuidgen &>/dev/null; then
        uuidgen | tr '[:upper:]' '[:lower:]'
    else
        cat /proc/sys/kernel/random/uuid
    fi
}

# Generate test user ID
# Usage: generate_user_id [prefix]
generate_user_id() {
    local prefix="${1:-test-user}"
    local uuid=$(generate_uuid | cut -d'-' -f1)
    echo "${prefix}-${uuid}"
}

# Generate test item ID
# Usage: generate_item_id [prefix]
generate_item_id() {
    local prefix="${1:-test-item}"
    local uuid=$(generate_uuid | cut -d'-' -f1)
    echo "${prefix}-${uuid}"
}

# Generate test account ID
# Usage: generate_account_id [prefix]
generate_account_id() {
    local prefix="${1:-test-acc}"
    local uuid=$(generate_uuid | cut -d'-' -f1)
    echo "${prefix}-${uuid}"
}

# Generate Plaid-like transaction ID
# Usage: generate_plaid_transaction_id
generate_plaid_transaction_id() {
    local uuid=$(generate_uuid)
    echo "plaid-txn-${uuid}"
}

# Generate Plaid-like item ID
# Usage: generate_plaid_item_id
generate_plaid_item_id() {
    local uuid=$(generate_uuid)
    echo "plaid-item-${uuid}"
}

# Generate Plaid-like account ID
# Usage: generate_plaid_account_id
generate_plaid_account_id() {
    local uuid=$(generate_uuid)
    echo "plaid-acc-${uuid}"
}

# ============================================================================
# Date Generators
# ============================================================================

# Generate random date in the past N days
# Usage: generate_date_in_past_days <days>
generate_date_in_past_days() {
    local days="$1"

    if [[ -z "$days" ]]; then
        log_error "generate_date_in_past_days: days parameter required"
        return 1
    fi

    # Random number of days ago (0 to days)
    local days_ago=$((RANDOM % days))

    # Generate date (works on both Linux and macOS)
    if date --version &>/dev/null 2>&1; then
        # GNU date (Linux)
        date -d "$days_ago days ago" +%Y-%m-%d
    else
        # BSD date (macOS)
        date -v-${days_ago}d +%Y-%m-%d
    fi
}

# Generate random date in range
# Usage: generate_date_in_range <start_date> <end_date>
generate_date_in_range() {
    local start_date="$1"
    local end_date="$2"

    if [[ -z "$start_date" ]] || [[ -z "$end_date" ]]; then
        log_error "generate_date_in_range: start_date and end_date required"
        return 1
    fi

    # Convert to epoch seconds
    local start_epoch=$(date -d "$start_date" +%s 2>/dev/null || date -j -f "%Y-%m-%d" "$start_date" +%s)
    local end_epoch=$(date -d "$end_date" +%s 2>/dev/null || date -j -f "%Y-%m-%d" "$end_date" +%s)

    # Random epoch between start and end
    local range=$((end_epoch - start_epoch))
    local random_offset=$((RANDOM % range))
    local random_epoch=$((start_epoch + random_offset))

    # Convert back to date
    if date --version &>/dev/null 2>&1; then
        date -d "@$random_epoch" +%Y-%m-%d
    else
        date -r "$random_epoch" +%Y-%m-%d
    fi
}

# Generate date for tier-specific historical range
# Usage: generate_date_for_tier <tier>
# Returns: Date within tier's historical range
generate_date_for_tier() {
    local tier="$1"

    case "$tier" in
        free)
            generate_date_in_past_days 30
            ;;
        premium_lite)
            generate_date_in_past_days 365
            ;;
        premium)
            generate_date_in_past_days 730
            ;;
        premium_plus)
            generate_date_in_past_days 730
            ;;
        *)
            log_error "Unknown tier: $tier"
            return 1
            ;;
    esac
}

# ============================================================================
# Amount Generators
# ============================================================================

# Generate random transaction amount
# Usage: generate_amount [min] [max]
# Returns: Decimal amount (e.g., 45.67)
generate_amount() {
    local min="${1:-1}"
    local max="${2:-500}"

    # Generate random integer between min and max
    local int_part=$((min + RANDOM % (max - min + 1)))

    # Generate random cents (00-99)
    local cents=$((RANDOM % 100))

    printf "%d.%02d" "$int_part" "$cents"
}

# Generate purchase amount (typical spending range)
# Usage: generate_purchase_amount
generate_purchase_amount() {
    # Most purchases are between $5-$200
    generate_amount 5 200
}

# Generate grocery amount
# Usage: generate_grocery_amount
generate_grocery_amount() {
    # Groceries typically $20-$150
    generate_amount 20 150
}

# Generate restaurant amount
# Usage: generate_restaurant_amount
generate_restaurant_amount() {
    # Restaurant bills typically $15-$80
    generate_amount 15 80
}

# Generate gas amount
# Usage: generate_gas_amount
generate_gas_amount() {
    # Gas purchases typically $30-$70
    generate_amount 30 70
}

# Generate paycheck amount
# Usage: generate_paycheck_amount
generate_paycheck_amount() {
    # Paychecks typically $1000-$5000
    generate_amount 1000 5000
}

# Generate refund amount
# Usage: generate_refund_amount
generate_refund_amount() {
    # Refunds typically $10-$300
    generate_amount 10 300
}

# ============================================================================
# Transaction Name Generators
# ============================================================================

# Merchant name lists
GROCERY_MERCHANTS=("Whole Foods" "Safeway" "Trader Joe's" "Target" "Walmart" "Kroger" "Publix" "Costco")
RESTAURANT_MERCHANTS=("Chipotle" "Starbucks" "McDonald's" "Panera Bread" "Subway" "Pizza Hut" "Olive Garden" "Red Lobster")
GAS_MERCHANTS=("Shell" "Chevron" "BP" "Exxon" "Mobil" "Arco" "76" "Valero")
RETAIL_MERCHANTS=("Amazon" "Best Buy" "Home Depot" "Lowe's" "Macy's" "Nordstrom" "Target" "Walmart")
UTILITY_MERCHANTS=("PG&E" "Comcast" "AT&T" "Verizon" "Pacific Gas" "Southern California Edison")
EMPLOYER_NAMES=("Acme Corp" "TechStart Inc" "Global Solutions" "Innovation Labs" "Digital Dynamics")

# Generate random merchant name by category
# Usage: generate_merchant_name <category>
generate_merchant_name() {
    local category="$1"

    case "$category" in
        grocery)
            local merchants=("${GROCERY_MERCHANTS[@]}")
            ;;
        restaurant)
            local merchants=("${RESTAURANT_MERCHANTS[@]}")
            ;;
        gas)
            local merchants=("${GAS_MERCHANTS[@]}")
            ;;
        retail)
            local merchants=("${RETAIL_MERCHANTS[@]}")
            ;;
        utility)
            local merchants=("${UTILITY_MERCHANTS[@]}")
            ;;
        employer)
            local merchants=("${EMPLOYER_NAMES[@]}")
            ;;
        *)
            log_error "Unknown merchant category: $category"
            return 1
            ;;
    esac

    # Select random merchant
    local index=$((RANDOM % ${#merchants[@]}))
    echo "${merchants[$index]}"
}

# Generate transaction description
# Usage: generate_transaction_description <merchant_name> [location]
generate_transaction_description() {
    local merchant="$1"
    local location="${2:-San Francisco CA}"

    echo "$merchant - $location"
}

# ============================================================================
# Transaction Type Generators
# ============================================================================

# Generate standard purchase transaction
# Usage: generate_purchase_transaction <user_id> <account_id> <linked_item_id> [date]
generate_purchase_transaction() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local date="${4:-$(generate_date_in_past_days 30)}"

    local external_id=$(generate_plaid_transaction_id)
    local amount=$(generate_purchase_amount)
    local merchant=$(generate_merchant_name "retail")
    local name=$(generate_transaction_description "$merchant")

    echo "$user_id|$account_id|$linked_item_id|$external_id|-$amount|$date|$name"
}

# Generate grocery transaction
# Usage: generate_grocery_transaction <user_id> <account_id> <linked_item_id> [date]
generate_grocery_transaction() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local date="${4:-$(generate_date_in_past_days 30)}"

    local external_id=$(generate_plaid_transaction_id)
    local amount=$(generate_grocery_amount)
    local merchant=$(generate_merchant_name "grocery")
    local name=$(generate_transaction_description "$merchant")

    echo "$user_id|$account_id|$linked_item_id|$external_id|-$amount|$date|$name"
}

# Generate paycheck transaction
# Usage: generate_paycheck_transaction <user_id> <account_id> <linked_item_id> [date]
generate_paycheck_transaction() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local date="${4:-$(generate_date_in_past_days 14)}"

    local external_id=$(generate_plaid_transaction_id)
    local amount=$(generate_paycheck_amount)
    local employer=$(generate_merchant_name "employer")
    local name="Direct Deposit - $employer"

    echo "$user_id|$account_id|$linked_item_id|$external_id|$amount|$date|$name"
}

# Generate refund transaction
# Usage: generate_refund_transaction <user_id> <account_id> <linked_item_id> [date]
generate_refund_transaction() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local date="${4:-$(generate_date_in_past_days 7)}"

    local external_id=$(generate_plaid_transaction_id)
    local amount=$(generate_refund_amount)
    local merchant=$(generate_merchant_name "retail")
    local name="REFUND - $merchant"

    echo "$user_id|$account_id|$linked_item_id|$external_id|$amount|$date|$name"
}

# ============================================================================
# Bulk Transaction Generators
# ============================================================================

# Generate multiple transactions for a user
# Usage: generate_transaction_set <user_id> <account_id> <linked_item_id> <count> [days_history]
# Returns: Pipe-delimited transaction data (one per line)
generate_transaction_set() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local count="$4"
    local days_history="${5:-30}"

    if [[ -z "$user_id" ]] || [[ -z "$account_id" ]] || [[ -z "$linked_item_id" ]] || [[ -z "$count" ]]; then
        log_error "generate_transaction_set: user_id, account_id, linked_item_id, and count required"
        return 1
    fi

    log_info "Generating $count transactions for user $user_id..."

    # Transaction type distribution (weighted)
    # 60% purchases, 20% groceries, 10% paychecks, 10% refunds
    local transactions=()

    for ((i=1; i<=count; i++)); do
        local date=$(generate_date_in_past_days "$days_history")
        local rand=$((RANDOM % 100))

        if [[ $rand -lt 60 ]]; then
            # Purchase (60%)
            generate_purchase_transaction "$user_id" "$account_id" "$linked_item_id" "$date"
        elif [[ $rand -lt 80 ]]; then
            # Grocery (20%)
            generate_grocery_transaction "$user_id" "$account_id" "$linked_item_id" "$date"
        elif [[ $rand -lt 90 ]]; then
            # Paycheck (10%)
            generate_paycheck_transaction "$user_id" "$account_id" "$linked_item_id" "$date"
        else
            # Refund (10%)
            generate_refund_transaction "$user_id" "$account_id" "$linked_item_id" "$date"
        fi
    done
}

# ============================================================================
# Account Balance Generators
# ============================================================================

# Generate realistic checking account balance
# Usage: generate_checking_balance
generate_checking_balance() {
    # Checking accounts typically $500-$5000
    generate_amount 500 5000
}

# Generate realistic savings account balance
# Usage: generate_savings_balance
generate_savings_balance() {
    # Savings accounts typically $1000-$20000
    generate_amount 1000 20000
}

# Generate realistic credit card balance
# Usage: generate_credit_card_balance
generate_credit_card_balance() {
    # Credit cards typically $0-$3000 (negative for what's owed)
    local amount=$(generate_amount 0 3000)
    echo "-$amount"
}

# ============================================================================
# Cursor Generators
# ============================================================================

# Generate mock Plaid cursor
# Usage: generate_plaid_cursor [page]
generate_plaid_cursor() {
    local page="${1:-1}"
    local uuid=$(generate_uuid)
    echo "cursor-page-${page}-${uuid}"
}

# ============================================================================
# Export Functions
# ============================================================================

export -f generate_uuid
export -f generate_user_id
export -f generate_item_id
export -f generate_account_id
export -f generate_plaid_transaction_id
export -f generate_plaid_item_id
export -f generate_plaid_account_id
export -f generate_date_in_past_days
export -f generate_date_in_range
export -f generate_date_for_tier
export -f generate_amount
export -f generate_purchase_amount
export -f generate_grocery_amount
export -f generate_restaurant_amount
export -f generate_gas_amount
export -f generate_paycheck_amount
export -f generate_refund_amount
export -f generate_merchant_name
export -f generate_transaction_description
export -f generate_purchase_transaction
export -f generate_grocery_transaction
export -f generate_paycheck_transaction
export -f generate_refund_transaction
export -f generate_transaction_set
export -f generate_checking_balance
export -f generate_savings_balance
export -f generate_credit_card_balance
export -f generate_plaid_cursor
