#!/bin/bash
# Database Utilities Library for BoostedBudget Test Scenarios
# Provides functions for database operations and test data setup

# Source logging functions from api-client.sh if not already loaded
if ! type -t log_info &>/dev/null; then
    source "$(dirname "${BASH_SOURCE[0]}")/api-client.sh"
fi

# Validate required database environment variables
validate_db_env() {
    if [[ -z "${DATABASE_URL:-}" ]]; then
        log_error "DATABASE_URL environment variable is required"
        log_error "Example: postgresql://user:pass@host:port/database"
        return 1
    fi

    log_info "Database environment validated"
    return 0
}

# Execute SQL query and return result
# Usage: query_db <sql_query>
# Returns: Query result as plain text
query_db() {
    local query="$1"

    if [[ -z "$query" ]]; then
        log_error "query_db: SQL query is required"
        return 1
    fi

    psql "${DATABASE_URL}" -t -A -c "$query" 2>&1
}

# Execute SQL query and return JSON result
# Usage: query_db_json <sql_query>
# Returns: Query result as JSON array
query_db_json() {
    local query="$1"

    if [[ -z "$query" ]]; then
        log_error "query_db_json: SQL query is required"
        return 1
    fi

    # Use PostgreSQL's json_agg to return JSON
    local json_query="SELECT COALESCE(json_agg(row_to_json(t)), '[]'::json) FROM ($query) t;"

    psql "${DATABASE_URL}" -t -A -c "$json_query" 2>&1
}

# Count rows matching condition
# Usage: count_rows <table> <where_clause>
# Returns: Integer count
count_rows() {
    local table="$1"
    local where_clause="${2:-1=1}"

    if [[ -z "$table" ]]; then
        log_error "count_rows: table name is required"
        return 1
    fi

    local count
    count=$(query_db "SELECT COUNT(*) FROM $table WHERE $where_clause")

    if [[ $? -eq 0 ]] && [[ "$count" =~ ^[0-9]+$ ]]; then
        echo "$count"
        return 0
    else
        log_error "Failed to count rows in $table"
        return 1
    fi
}

# Insert test user
# Usage: insert_user <user_id> <tier> <email>
# Returns: 0 on success, 1 on failure
insert_user() {
    local user_id="$1"
    local tier="$2"
    local email="$3"

    if [[ -z "$user_id" ]] || [[ -z "$tier" ]] || [[ -z "$email" ]]; then
        log_error "insert_user: user_id, tier, and email are required"
        return 1
    fi

    # Validate tier
    case "$tier" in
        free|premium_lite|premium|premium_plus)
            ;;
        *)
            log_error "Invalid tier: $tier (valid: free, premium_lite, premium, premium_plus)"
            return 1
            ;;
    esac

    log_info "Inserting test user: $user_id ($tier)"

    local result
    result=$(query_db "
        INSERT INTO users (id, auth0_user_id, email, subscription_tier, primary_region, created_at, updated_at)
        VALUES (
            '$user_id',
            'auth0|$user_id',
            '$email',
            '$tier',
            'us-east-1',
            NOW(),
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            subscription_tier = EXCLUDED.subscription_tier,
            email = EXCLUDED.email,
            updated_at = NOW()
        RETURNING id;
    ")

    if [[ $? -eq 0 ]] && [[ "$result" == "$user_id" ]]; then
        log_success "User created/updated: $user_id"
        return 0
    else
        log_error "Failed to insert user: $result"
        return 1
    fi
}

# Insert linked item (requires test access token)
# Usage: insert_linked_item <item_id> <user_id> <plaid_item_id> <institution_name> [cursor] [status]
# Note: Uses a test encrypted token (you'll need to provide real encrypted tokens for actual testing)
insert_linked_item() {
    local item_id="$1"
    local user_id="$2"
    local plaid_item_id="$3"
    local institution_name="$4"
    local cursor="${5:-NULL}"
    local status="${6:-active}"

    if [[ -z "$item_id" ]] || [[ -z "$user_id" ]] || [[ -z "$plaid_item_id" ]] || [[ -z "$institution_name" ]]; then
        log_error "insert_linked_item: item_id, user_id, plaid_item_id, and institution_name are required"
        return 1
    fi

    # TODO: For real testing, you need to provide actual encrypted access tokens
    # This is a placeholder that will need to be replaced
    local encrypted_token="ENCRYPTED_TOKEN_PLACEHOLDER_$plaid_item_id"

    log_info "Inserting linked item: $item_id (plaid_item: $plaid_item_id)"

    # Handle NULL cursor
    local cursor_value
    if [[ "$cursor" == "NULL" ]] || [[ -z "$cursor" ]]; then
        cursor_value="NULL"
    else
        cursor_value="'$cursor'"
    fi

    local result
    result=$(query_db "
        INSERT INTO linked_items (
            id, user_id, item_id, access_token_encrypted,
            institution_id, institution_name, provider,
            cursor, status, created_at, updated_at
        )
        VALUES (
            '$item_id',
            '$user_id',
            '$plaid_item_id',
            '$encrypted_token',
            'ins_3',
            '$institution_name',
            'plaid',
            $cursor_value,
            '$status',
            NOW(),
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            cursor = EXCLUDED.cursor,
            status = EXCLUDED.status,
            updated_at = NOW()
        RETURNING id;
    ")

    if [[ $? -eq 0 ]] && [[ "$result" == "$item_id" ]]; then
        log_success "Linked item created/updated: $item_id"
        return 0
    else
        log_error "Failed to insert linked item: $result"
        return 1
    fi
}

# Insert account
# Usage: insert_account <account_id> <linked_item_id> <user_id> <plaid_account_id> <name> <type> <subtype> <balance>
insert_account() {
    local account_id="$1"
    local linked_item_id="$2"
    local user_id="$3"
    local plaid_account_id="$4"
    local name="$5"
    local type="$6"
    local subtype="$7"
    local balance="$8"

    if [[ -z "$account_id" ]] || [[ -z "$linked_item_id" ]] || [[ -z "$user_id" ]] || \
       [[ -z "$plaid_account_id" ]] || [[ -z "$name" ]] || [[ -z "$type" ]]; then
        log_error "insert_account: account_id, linked_item_id, user_id, plaid_account_id, name, and type are required"
        return 1
    fi

    log_info "Inserting account: $account_id ($name)"

    local result
    result=$(query_db "
        INSERT INTO accounts (
            id, linked_item_id, user_id, plaid_account_id,
            name, official_name, type, subtype, mask,
            current_balance, available_balance, currency_code,
            created_at, updated_at
        )
        VALUES (
            '$account_id',
            '$linked_item_id',
            '$user_id',
            '$plaid_account_id',
            '$name',
            '$name',
            '$type',
            '${subtype:-checking}',
            '0000',
            ${balance:-0.00},
            ${balance:-0.00},
            'USD',
            NOW(),
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            current_balance = EXCLUDED.current_balance,
            available_balance = EXCLUDED.available_balance,
            updated_at = NOW()
        RETURNING id;
    ")

    if [[ $? -eq 0 ]] && [[ "$result" == "$account_id" ]]; then
        log_success "Account created/updated: $account_id"
        return 0
    else
        log_error "Failed to insert account: $result"
        return 1
    fi
}

# Insert transaction (for testing updates/modifications)
# Usage: insert_transaction <user_id> <account_id> <linked_item_id> <external_id> <amount> <date> <name> [user_notes]
insert_transaction() {
    local user_id="$1"
    local account_id="$2"
    local linked_item_id="$3"
    local external_id="$4"
    local amount="$5"
    local date="$6"
    local name="$7"
    local user_notes="${8:-NULL}"

    if [[ -z "$user_id" ]] || [[ -z "$account_id" ]] || [[ -z "$linked_item_id" ]] || \
       [[ -z "$external_id" ]] || [[ -z "$amount" ]] || [[ -z "$date" ]] || [[ -z "$name" ]]; then
        log_error "insert_transaction: user_id, account_id, linked_item_id, external_id, amount, date, and name are required"
        return 1
    fi

    log_info "Inserting transaction: $external_id ($name)"

    # Handle NULL user_notes
    local notes_value
    if [[ "$user_notes" == "NULL" ]] || [[ -z "$user_notes" ]]; then
        notes_value="NULL"
    else
        notes_value="'$user_notes'"
    fi

    local result
    result=$(query_db "
        INSERT INTO transactions (
            user_id, account_id, linked_item_id,
            source, external_transaction_id,
            amount, date, name,
            payment_channel, pending, user_notes,
            created_at, updated_at
        )
        VALUES (
            '$user_id',
            '$account_id',
            '$linked_item_id',
            'plaid',
            '$external_id',
            $amount,
            '$date',
            '$name',
            'online',
            false,
            $notes_value,
            NOW(),
            NOW()
        )
        ON CONFLICT (source, external_transaction_id, user_id) DO UPDATE SET
            amount = EXCLUDED.amount,
            date = EXCLUDED.date,
            updated_at = NOW()
        RETURNING id;
    ")

    if [[ $? -eq 0 ]]; then
        log_success "Transaction created/updated: $external_id"
        return 0
    else
        log_error "Failed to insert transaction: $result"
        return 1
    fi
}

# Get transaction count for user
# Usage: get_transaction_count <user_id>
# Returns: Number of transactions
get_transaction_count() {
    local user_id="$1"

    if [[ -z "$user_id" ]]; then
        log_error "get_transaction_count: user_id is required"
        return 1
    fi

    count_rows "transactions" "user_id = '$user_id'"
}

# Get sync audit count for user and month
# Usage: get_sync_audit_count <user_id> [month]
# Returns: Number of sync audits
get_sync_audit_count() {
    local user_id="$1"
    local month="${2:-$(date +%Y-%m)}"

    if [[ -z "$user_id" ]]; then
        log_error "get_sync_audit_count: user_id is required"
        return 1
    fi

    count_rows "account_sync_audit" "user_id = '$user_id' AND sync_month = '$month'"
}

# Get user notes for a transaction
# Usage: get_user_notes <external_transaction_id> <user_id>
# Returns: User notes or empty string
get_user_notes() {
    local external_id="$1"
    local user_id="$2"

    if [[ -z "$external_id" ]] || [[ -z "$user_id" ]]; then
        log_error "get_user_notes: external_transaction_id and user_id are required"
        return 1
    fi

    query_db "SELECT COALESCE(user_notes, '') FROM transactions WHERE external_transaction_id = '$external_id' AND user_id = '$user_id'"
}

# Get linked item cursor
# Usage: get_linked_item_cursor <item_id>
# Returns: Cursor value or empty string
get_linked_item_cursor() {
    local item_id="$1"

    if [[ -z "$item_id" ]]; then
        log_error "get_linked_item_cursor: item_id is required"
        return 1
    fi

    query_db "SELECT COALESCE(cursor, '') FROM linked_items WHERE id = '$item_id'"
}

# Get linked item status
# Usage: get_linked_item_status <item_id>
# Returns: Status value
get_linked_item_status() {
    local item_id="$1"

    if [[ -z "$item_id" ]]; then
        log_error "get_linked_item_status: item_id is required"
        return 1
    fi

    query_db "SELECT status FROM linked_items WHERE id = '$item_id'"
}

# Clean up test data for a user
# Usage: cleanup_test_user <user_id>
# Returns: 0 on success
cleanup_test_user() {
    local user_id="$1"

    if [[ -z "$user_id" ]]; then
        log_error "cleanup_test_user: user_id is required"
        return 1
    fi

    log_info "Cleaning up test data for user: $user_id"

    # Delete in reverse foreign key order
    query_db "DELETE FROM transactions WHERE user_id = '$user_id';" >/dev/null
    query_db "DELETE FROM account_sync_audit WHERE user_id = '$user_id';" >/dev/null
    query_db "DELETE FROM notifications WHERE user_id = '$user_id';" >/dev/null
    query_db "DELETE FROM accounts WHERE user_id = '$user_id';" >/dev/null
    query_db "DELETE FROM linked_items WHERE user_id = '$user_id';" >/dev/null
    query_db "DELETE FROM users WHERE id = '$user_id';" >/dev/null

    log_success "Test data cleaned up for user: $user_id"
    return 0
}

# Test database connection
# Usage: test_db_connection
# Returns: 0 if connected, 1 if not
test_db_connection() {
    log_info "Testing database connection..."

    local result
    result=$(query_db "SELECT 1;" 2>&1)

    if [[ "$result" == "1" ]]; then
        log_success "Database connection successful"
        return 0
    else
        log_error "Database connection failed: $result"
        return 1
    fi
}

# Get database statistics
# Usage: get_db_stats
# Returns: JSON with table row counts
get_db_stats() {
    log_info "Fetching database statistics..."

    local users_count
    local linked_items_count
    local accounts_count
    local transactions_count
    local webhooks_count

    users_count=$(count_rows "users")
    linked_items_count=$(count_rows "linked_items")
    accounts_count=$(count_rows "accounts")
    transactions_count=$(count_rows "transactions")
    webhooks_count=$(count_rows "webhook_events")

    cat <<EOF
{
  "users": $users_count,
  "linked_items": $linked_items_count,
  "accounts": $accounts_count,
  "transactions": $transactions_count,
  "webhooks": $webhooks_count
}
EOF
}

# Export functions
export -f validate_db_env
export -f query_db
export -f query_db_json
export -f count_rows
export -f insert_user
export -f insert_linked_item
export -f insert_account
export -f insert_transaction
export -f get_transaction_count
export -f get_sync_audit_count
export -f get_user_notes
export -f get_linked_item_cursor
export -f get_linked_item_status
export -f cleanup_test_user
export -f test_db_connection
export -f get_db_stats
