#!/bin/bash

# fake-data-generator.sh - Realistic transaction generation
# Generates or inserts transactions based on scenario configuration

# Escape single quotes for SQL (replace ' with '')
sql_escape() {
  echo "$1" | sed "s/'/''/g"
}

# Merchant pools for realistic data
declare -a EXPENSE_MERCHANTS=(
  "Starbucks" "Amazon" "Shell Gas Station" "Whole Foods Market" "Netflix"
  "Spotify" "Uber" "Lyft" "Target" "CVS Pharmacy" "McDonald's" "Chipotle"
  "Home Depot" "Lowe's" "Best Buy" "Apple Store" "AT&T" "Verizon" "Comcast"
  "Walmart" "Costco" "Safeway" "Kroger" "Trader Joe's" "Peet's Coffee"
  "Blue Bottle Coffee" "Panera Bread" "Subway" "Taco Bell" "Pizza Hut"
  "Domino's Pizza" "FedEx" "UPS Store" "USPS" "Walgreens" "Rite Aid"
  "PetSmart" "Petco" "REI" "Dick's Sporting Goods" "Nike" "Adidas"
  "Gap" "Old Navy" "H&M" "Zara" "Nordstrom" "Macy's" "Sephora" "Ulta"
)

declare -a INCOME_SOURCES=(
  "Direct Deposit - Payroll"
  "Direct Deposit - Salary"
  "Paycheck"
  "Freelance Payment"
  "Consulting Fee"
)

declare -a CATEGORIES=(
  "Food and Drink" "Shopping" "Transportation" "Entertainment"
  "Gas" "Groceries" "Healthcare" "Utilities" "Home"
  "Travel" "Personal Care" "Recreation" "Subscriptions"
)

# Map user-facing category to Plaid primary category enum
# Used to populate personal_finance_category JSONB field
get_plaid_primary_category() {
  local category="$1"
  case "$category" in
    "Food and Drink"|"Groceries") echo "FOOD_AND_DRINK" ;;
    "Shopping") echo "SHOPPING" ;;
    "Transportation"|"Gas") echo "TRANSPORTATION" ;;
    "Entertainment"|"Recreation") echo "ENTERTAINMENT" ;;
    "Healthcare") echo "HEALTHCARE" ;;
    "Utilities") echo "UTILITIES" ;;
    "Home") echo "HOME_IMPROVEMENT" ;;
    "Travel") echo "TRAVEL" ;;
    "Personal Care") echo "PERSONAL_CARE" ;;
    "Subscriptions") echo "RENT_AND_UTILITIES" ;;
    "Income") echo "INCOME" ;;
    *) echo "GENERAL_MERCHANDISE" ;;
  esac
}

# Generate random merchant name
get_random_merchant() {
  echo "${EXPENSE_MERCHANTS[$RANDOM % ${#EXPENSE_MERCHANTS[@]}]}"
}

# Generate random income source
get_random_income_source() {
  echo "${INCOME_SOURCES[$RANDOM % ${#INCOME_SOURCES[@]}]}"
}

# Generate random category
get_random_category() {
  echo "${CATEGORIES[$RANDOM % ${#CATEGORIES[@]}]}"
}

# Generate random amount (expenses are negative)
generate_random_amount() {
  local is_income=$1

  if [ "$is_income" = "true" ]; then
    # Income: $2000-$5000
    awk 'BEGIN{srand(); printf "%.2f", 2000+rand()*3000}'
  else
    # Expense: -$5 to -$200
    awk 'BEGIN{srand(); printf "-%.2f", 5+rand()*195}'
  fi
}

# Generate random date within historical period
generate_random_date() {
  local days_back=$((RANDOM % HISTORICAL_DAYS))

  # macOS vs Linux date command compatibility
  if date -v-1d > /dev/null 2>&1; then
    # macOS
    date -v-${days_back}d +%Y-%m-%d
  else
    # Linux
    date -d "$days_back days ago" +%Y-%m-%d
  fi
}

# Generate single fake transaction
generate_fake_transaction() {
  local account_id=$1
  local linked_item_id=$2

  # 10% chance of income, 90% expense
  local is_income="false"
  if [ $((RANDOM % 10)) -eq 0 ]; then
    is_income="true"
  fi

  # Generate transaction data
  local merchant
  if [ "$is_income" = "true" ]; then
    merchant=$(get_random_income_source)
    local category="Income"
  else
    merchant=$(get_random_merchant)
    local category=$(get_random_category)
  fi

  local amount
  amount=$(generate_random_amount "$is_income")
  local date
  date=$(generate_random_date)

  # 10% chance of pending
  local pending="false"
  if [ $((RANDOM % 10)) -eq 0 ]; then
    pending="true"
  fi

  # Generate IDs
  local txn_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
  local external_id="fake-txn-$(uuidgen)"

  # Escape SQL special characters
  local escaped_merchant=$(sql_escape "$merchant")

  # Get Plaid primary category for personal_finance_category JSONB
  local plaid_cat
  plaid_cat=$(get_plaid_primary_category "$category")

  # Insert transaction
  local sql="
    INSERT INTO transactions (
      id, user_id, account_id, linked_item_id, source,
      external_transaction_id, amount, date, authorized_date, name,
      category, personal_finance_category, pending, created_at, updated_at
    )
    VALUES (
      '$txn_id',
      '$USER_ID',
      '$account_id',
      '$linked_item_id',
      'plaid',
      '$external_id',
      $amount,
      '$date',
      '$date',
      '$escaped_merchant',
      to_jsonb(ARRAY['$category']),
      jsonb_build_object('primary', '$plaid_cat'),
      $pending,
      NOW(),
      NOW()
    );
  "

  execute_db "$sql"
}

# Insert manual transaction from YAML
insert_manual_transaction() {
  local index=$1

  local account_index
  account_index=$(get_transaction_account_index "$index")

  if [ "$account_index" = "null" ] || [ -z "$account_index" ]; then
    log_error "account_index required for transactions[$index]"
    return 1
  fi

  # Validate account_index
  if [ "$account_index" -ge "${#ACCOUNT_IDS[@]}" ]; then
    log_error "Invalid account_index $account_index for transactions[$index]"
    return 1
  fi

  local account_id="${ACCOUNT_IDS[$account_index]}"
  local linked_item_id="${ACCOUNT_LINKED_ITEM_IDS[$account_index]}"

  local amount
  amount=$(get_transaction_amount "$index")
  local date
  date=$(get_transaction_date "$index")
  local name
  name=$(get_transaction_name "$index")
  local pending
  pending=$(get_transaction_pending "$index")
  local user_notes
  user_notes=$(get_transaction_user_notes "$index")
  local category
  category=$(get_transaction_category "$index")

  # Validate required fields
  if [ "$amount" = "null" ] || [ -z "$amount" ]; then
    log_error "amount required for transactions[$index]"
    return 1
  fi

  if [ "$date" = "null" ] || [ -z "$date" ]; then
    log_error "date required for transactions[$index]"
    return 1
  fi

  if [ "$name" = "null" ] || [ -z "$name" ]; then
    log_error "name required for transactions[$index]"
    return 1
  fi

  # Generate IDs
  local txn_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
  local external_id="manual-txn-$(uuidgen)"

  log_debug "Inserting manual transaction[$index]: $name ($amount)"

  # Escape SQL special characters
  local escaped_name=$(sql_escape "$name")
  local escaped_notes=$(sql_escape "$user_notes")

  # Get Plaid primary category for personal_finance_category JSONB
  local plaid_cat
  plaid_cat=$(get_plaid_primary_category "$category")

  # Build SQL with optional user_notes
  local sql="
    INSERT INTO transactions (
      id, user_id, account_id, linked_item_id, source,
      external_transaction_id, amount, date, authorized_date, name,
      category, personal_finance_category, pending, user_notes, created_at, updated_at
    )
    VALUES (
      '$txn_id',
      '$USER_ID',
      '$account_id',
      '$linked_item_id',
      'plaid',
      '$external_id',
      $amount,
      '$date',
      '$date',
      '$escaped_name',
      to_jsonb(ARRAY['$category']),
      jsonb_build_object('primary', '$plaid_cat'),
      $pending,
      $([ -n "$user_notes" ] && echo "'$escaped_notes'" || echo "NULL"),
      NOW(),
      NOW()
    );
  "

  execute_db "$sql"
}

# Generate transactions (main entry point)
generate_transactions() {
  log_info "Generating transactions..."

  # Determine transaction mode
  local mode
  mode=$(get_transaction_mode)

  case "$mode" in
    "auto")
      generate_auto_transactions
      ;;
    "manual")
      generate_manual_transactions
      ;;
    "none")
      log_info "No transactions defined in scenario"
      return 0
      ;;
    *)
      log_error "Unknown transaction mode: $mode"
      return 1
      ;;
  esac
}

# Generate auto transactions (count_per_account)
generate_auto_transactions() {
  local count_per_account
  count_per_account=$(get_transaction_count_per_account)

  if [ "$count_per_account" = "null" ] || [ "$count_per_account" -le 0 ]; then
    log_error "Invalid count_per_account: $count_per_account"
    return 1
  fi

  local account_count=${#ACCOUNT_IDS[@]}
  local total_transactions=$((count_per_account * account_count))

  log_info "Generating $count_per_account transactions per account ($total_transactions total)"

  local generated=0
  for i in $(seq 0 $((account_count - 1))); do
    local account_id="${ACCOUNT_IDS[$i]}"
    local linked_item_id="${ACCOUNT_LINKED_ITEM_IDS[$i]}"

    for j in $(seq 1 "$count_per_account"); do
      if generate_fake_transaction "$account_id" "$linked_item_id"; then
        ((generated++))
        if [ $((generated % 20)) -eq 0 ]; then
          log_progress "$generated" "$total_transactions" "transactions"
        fi
      else
        log_error "Failed to generate transaction $generated"
        return 1
      fi
    done
  done

  log_success "Generated $generated transaction(s)"
  return 0
}

# Generate manual transactions (defined in YAML)
generate_manual_transactions() {
  local count
  count=$(get_manual_transactions_count)

  if [ "$count" = "0" ]; then
    log_info "No manual transactions defined"
    return 0
  fi

  log_info "Inserting $count manual transaction(s)"

  local inserted=0
  for i in $(seq 0 $((count - 1))); do
    if insert_manual_transaction "$i"; then
      ((inserted++))
      log_progress "$inserted" "$count" "manual transactions"
    else
      log_error "Failed to insert manual transaction[$i]"
      return 1
    fi
  done

  log_success "Inserted $inserted manual transaction(s)"
  return 0
}

# Verify transactions were created
verify_transactions_created() {
  local actual_count
  actual_count=$(count_transactions_by_user_id "$USER_ID")

  log_debug "Transactions created: $actual_count"
  return 0
}
