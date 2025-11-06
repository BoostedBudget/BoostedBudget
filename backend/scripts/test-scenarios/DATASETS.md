# Test Scenarios Datasets Guide

## Overview

This guide documents the templates, fixtures, and dataset patterns available for creating realistic test scenarios in the BoostedBudget test framework.

**Purpose**: Enable configuration-driven test data generation without writing code.

**Key Components**:
- **Templates** (`templates/`): Reusable YAML anchors for users, accounts, transactions, merchants
- **Fixtures** (`lib/fixtures.sh`): Bash functions for programmatic data generation
- **Scenarios** (`scenarios/`): Complete test cases combining templates and custom data

---

## Templates Directory

Located in `templates/`, these YAML files provide reusable data patterns via YAML anchors.

### 1. `users.yaml` - User Tier Templates

**Purpose**: Define user profiles with tier-specific limits and configurations.

**Available Templates**:

```yaml
# Free Tier (30 days historical)
user_free: &user_free
  tier: "free"
  rules_limit: 1
  accounts_limit: 3
  sync_frequency: "daily"
  historical_days: 30
  ads_enabled: true

# Premium Lite (365 days historical)
user_premium_lite: &user_premium_lite
  tier: "premium_lite"
  rules_limit: 5
  accounts_limit: 15
  sync_frequency: "twice_daily"
  historical_days: 365
  ads_enabled: false

# Premium (730 days historical)
user_premium: &user_premium
  tier: "premium"
  rules_limit: 15
  accounts_limit: 30
  sync_frequency: "six_times_daily"
  historical_days: 730
  ads_enabled: false

# Premium Plus (730 days + API access)
user_premium_plus: &user_premium_plus
  tier: "premium_plus"
  rules_limit: -1  # Unlimited
  accounts_limit: -1  # Unlimited
  sync_frequency: "every_10_minutes"
  historical_days: 730
  ads_enabled: false
  api_access: true
```

**Usage in Scenarios**:

```yaml
setup:
  user:
    <<: *user_premium  # Reference premium template
    id: "test-user-001"
    email: "premium@example.com"
```

**When to Use**:
- Testing tier-specific limits (e.g., free tier 3-account limit)
- Verifying subscription enforcement
- Testing historical data depth by tier

---

### 2. `accounts.yaml` - Account Type Templates

**Purpose**: Define realistic account structures for different financial institutions and account types.

**Available Templates**:

```yaml
# Checking Account
checking_account: &checking_account
  type: "depository"
  subtype: "checking"
  balance_range: [500, 5000]
  transaction_frequency: "high"  # Daily transactions

# Savings Account
savings_account: &savings_account
  type: "depository"
  subtype: "savings"
  balance_range: [1000, 50000]
  transaction_frequency: "low"  # Weekly/monthly

# Credit Card
credit_card: &credit_card
  type: "credit"
  subtype: "credit_card"
  balance_range: [-5000, 0]  # Negative = owed
  transaction_frequency: "high"

# Investment Account
investment_account: &investment_account
  type: "investment"
  subtype: "brokerage"
  balance_range: [5000, 100000]
  transaction_frequency: "low"

# Mortgage Loan
mortgage_loan: &mortgage_loan
  type: "loan"
  subtype: "mortgage"
  balance_range: [-500000, -100000]  # Negative = owed
  transaction_frequency: "very_low"  # Monthly
```

**Institution Examples**:

```yaml
# Chase Checking
chase_checking: &chase_checking
  <<: *checking_account
  institution_name: "Chase Bank"
  institution_id: "ins_3"

# Bank of America Savings
bofa_savings: &bofa_savings
  <<: *savings_account
  institution_name: "Bank of America"
  institution_id: "ins_4"

# Capital One Credit Card
capital_one_credit: &capital_one_credit
  <<: *credit_card
  institution_name: "Capital One"
  institution_id: "ins_127"
```

**Usage in Scenarios**:

```yaml
setup:
  accounts:
    - id: "acct-001"
      <<: *chase_checking
      name: "Chase Total Checking"
      current_balance: 3250.00

    - id: "acct-002"
      <<: *bofa_savings
      name: "BofA Savings"
      current_balance: 15000.00
```

**When to Use**:
- Creating multi-account scenarios
- Testing inter-account transfers (must net to zero)
- Simulating realistic account portfolios

---

### 3. `transactions.yaml` - Transaction Pattern Templates

**Purpose**: Define common transaction patterns and transaction sets for scenarios.

**Available Templates**:

```yaml
# Standard Purchase
standard_purchase: &standard_purchase
  amount_range: [-200, -5]
  payment_channel: "online"
  pending: false

# Pending Transaction
pending_transaction: &pending_transaction
  payment_channel: "in_store"
  pending: true
  authorized_date: "2024-01-15"

# Refund/Credit
refund_credit: &refund_credit
  amount_range: [5, 500]  # Positive amounts
  payment_channel: "online"
  pending: false

# Paycheck (Direct Deposit)
paycheck: &paycheck
  amount_range: [1000, 5000]  # Positive
  payment_channel: "other"
  name_pattern: "Direct Deposit"
  category:
    primary: "INCOME"
    detailed: "INCOME_WAGES"

# Subscription (Recurring)
subscription: &subscription
  amount_range: [-50, -5]
  payment_channel: "online"
  pending: false
  recurring: true
  cadence_days: 30
```

**Transaction Sets** (Bulk Data):

```yaml
# Free Tier Monthly Spending (30 days)
monthly_spending_free: &monthly_spending_free
  transaction_count: 50
  date_range_days: 30
  distribution:
    groceries: 0.25      # 25% groceries
    restaurants: 0.20    # 20% restaurants
    gas: 0.10           # 10% gas
    shopping: 0.25      # 25% shopping
    subscriptions: 0.05 # 5% subscriptions
    utilities: 0.05     # 5% utilities
    income: 0.05        # 5% paychecks
    other: 0.05         # 5% misc

# Premium Historical Backfill (730 days)
historical_premium: &historical_premium
  transaction_count: 1000
  date_range_days: 730
  distribution:
    # Same distribution as monthly_spending_free
```

**Usage in Scenarios**:

```yaml
setup:
  transactions:
    # Single refund
    - external_transaction_id: "refund-001"
      <<: *refund_credit
      amount: 129.99
      date: "2024-01-10"
      name: "Amazon.com - Refund"

    # Recurring subscription
    - external_transaction_id: "netflix-jan"
      <<: *subscription
      amount: -15.99
      name: "Netflix"
      date: "2024-01-15"
```

**When to Use**:
- Pre-populating transactions for testing updates/modifications
- Creating realistic spending patterns
- Testing refund scenarios
- Simulating recurring subscriptions

---

### 4. `merchants.yaml` - Merchant Database

**Purpose**: Provide 100+ realistic merchant names organized by Plaid Personal Finance Categories.

**Structure**:

```yaml
merchants:
  # FOOD_AND_DRINK Category
  food_and_drink:
    groceries:
      - &whole_foods
        name: "Whole Foods Market"
        typical_amount_range: [50, 200]
        personal_finance_category:
          primary: "FOOD_AND_DRINK"
          detailed: "FOOD_AND_DRINK_GROCERIES"

      - &trader_joes
        name: "Trader Joe's"
        typical_amount_range: [30, 120]
        personal_finance_category:
          primary: "FOOD_AND_DRINK"
          detailed: "FOOD_AND_DRINK_GROCERIES"

    restaurants:
      - &chipotle
        name: "Chipotle Mexican Grill"
        typical_amount_range: [10, 25]
        personal_finance_category:
          primary: "FOOD_AND_DRINK"
          detailed: "FOOD_AND_DRINK_RESTAURANTS"

    coffee:
      - &starbucks
        name: "Starbucks"
        typical_amount_range: [4, 8]
        personal_finance_category:
          primary: "FOOD_AND_DRINK"
          detailed: "FOOD_AND_DRINK_COFFEE"

  # TRANSPORTATION Category
  transportation:
    gas:
      - &shell
        name: "Shell"
        typical_amount_range: [35, 75]
        personal_finance_category:
          primary: "TRANSPORTATION"
          detailed: "TRANSPORTATION_GAS"

    rideshare:
      - &uber
        name: "Uber"
        typical_amount_range: [10, 40]
        personal_finance_category:
          primary: "TRANSPORTATION"
          detailed: "TRANSPORTATION_RIDE_SHARE"

  # GENERAL_MERCHANDISE Category
  general_merchandise:
    online_shopping:
      - &amazon
        name: "Amazon.com"
        typical_amount_range: [15, 300]
        personal_finance_category:
          primary: "GENERAL_MERCHANDISE"
          detailed: "GENERAL_MERCHANDISE_ONLINE_MARKETPLACES"

    electronics:
      - &best_buy
        name: "Best Buy"
        typical_amount_range: [50, 1500]
        personal_finance_category:
          primary: "GENERAL_MERCHANDISE"
          detailed: "GENERAL_MERCHANDISE_ELECTRONICS"

  # GENERAL_SERVICES Category
  general_services:
    subscriptions:
      - &netflix
        name: "Netflix"
        typical_amount_range: [15.99, 15.99]  # Fixed price
        personal_finance_category:
          primary: "GENERAL_SERVICES"
          detailed: "GENERAL_SERVICES_ENTERTAINMENT"
        recurring: true
        cadence_days: 30
```

**Available Categories** (16 primary Plaid categories):

1. `FOOD_AND_DRINK` (groceries, restaurants, coffee, bars, fast_food)
2. `TRANSPORTATION` (gas, parking, rideshare, public_transit, automotive)
3. `GENERAL_MERCHANDISE` (online_shopping, electronics, clothing, home_improvement)
4. `GENERAL_SERVICES` (subscriptions, entertainment, telecom, professional_services)
5. `HOME_IMPROVEMENT` (hardware, furniture, home_goods)
6. `MEDICAL` (hospitals, pharmacies, dental, vision)
7. `PERSONAL_CARE` (salons, gyms, spas)
8. `RENT_AND_UTILITIES` (rent, utilities, internet, phone)
9. `TRAVEL` (airlines, hotels, vacation_rentals)
10. `ENTERTAINMENT` (movies, concerts, sporting_events)
11. `INCOME` (paycheck, interest, dividends)
12. `TRANSFER_IN` / `TRANSFER_OUT` (inter-account transfers)
13. `LOAN_PAYMENTS` (mortgage, auto, student)
14. `BANK_FEES` (overdraft, monthly_fees, atm)
15. `GOVERNMENT_AND_NON_PROFIT` (taxes, donations)
16. `OTHER` (uncategorized)

**Usage in Scenarios**:

```yaml
transactions:
  # Reference specific merchant
  - external_transaction_id: "txn-001"
    <<: *starbucks  # Import name, amount range, category
    amount: -4.85
    date: "2024-01-15"

  # Override merchant amount
  - external_transaction_id: "txn-002"
    <<: *amazon
    amount: -129.99  # Override typical range
    date: "2024-01-16"
    name: "Amazon.com - Laptop Stand"
```

**When to Use**:
- Creating realistic monthly spending patterns (Scenario 12)
- Testing merchant diversity (Scenario 15)
- Simulating recurring subscriptions (Scenario 13)
- Testing category-based budgets (future Phase 3)

**Merchant Selection Best Practices**:
- **Diversity**: Use 20+ unique merchants for realistic scenarios
- **Repetition**: Some merchants (coffee, gas) appear frequently
- **Category Balance**: Distribute across multiple categories
- **Recurring**: Netflix, Spotify, gym memberships = monthly cadence

---

## Fixtures Library (`lib/fixtures.sh`)

Bash functions for **programmatic** dataset generation (alternative to YAML templates).

### Core Functions

#### ID Generators

```bash
# Generate UUIDs
generate_uuid                          # ’ "a3b4c5d6-e7f8-9012-3456-789abcdef012"
generate_user_id "premium"             # ’ "premium-a3b4c5d6"
generate_item_id "chase"               # ’ "chase-a3b4c5d6"
generate_account_id "checking"         # ’ "checking-a3b4c5d6"
generate_plaid_transaction_id         # ’ "plaid-txn-a3b4c5d6-..."
```

#### Date Generators

```bash
# Random dates
generate_date_in_past_days 30          # ’ "2024-12-15" (0-30 days ago)
generate_date_in_range "2024-01-01" "2024-12-31"  # ’ Random date in range

# Tier-specific dates
generate_date_for_tier "free"          # ’ Date within 30 days
generate_date_for_tier "premium"       # ’ Date within 730 days
```

#### Amount Generators

```bash
# Generic amounts
generate_amount 10 500                 # ’ "127.45" (random $10-$500)

# Category-specific amounts
generate_purchase_amount               # ’ "84.23" ($5-$200)
generate_grocery_amount                # ’ "127.89" ($20-$150)
generate_restaurant_amount             # ’ "42.15" ($15-$80)
generate_gas_amount                    # ’ "55.73" ($30-$70)
generate_paycheck_amount               # ’ "3214.50" ($1000-$5000)
generate_refund_amount                 # ’ "89.99" ($10-$300)
```

#### Balance Generators

```bash
generate_checking_balance              # ’ "2847.32" ($500-$5000)
generate_savings_balance               # ’ "15432.18" ($1000-$20000)
generate_credit_card_balance           # ’ "-1234.56" (-$3000 to $0)
```

#### Merchant Generators

```bash
generate_merchant_name "grocery"       # ’ "Whole Foods" or "Safeway" (random)
generate_merchant_name "restaurant"    # ’ "Chipotle" or "Panera Bread"
generate_merchant_name "gas"           # ’ "Shell" or "Chevron"
generate_merchant_name "retail"        # ’ "Amazon" or "Target"
```

**Available Categories**: `grocery`, `restaurant`, `gas`, `retail`, `utility`, `employer`

#### Transaction Generators

```bash
# Generate pipe-delimited transaction data
generate_purchase_transaction "$user_id" "$account_id" "$item_id" "2024-01-15"
# ’ "user|account|item|txn-id|-127.45|2024-01-15|Amazon.com - San Francisco CA"

generate_grocery_transaction "$user_id" "$account_id" "$item_id"
# ’ "user|account|item|txn-id|-89.23|2024-01-10|Whole Foods - San Francisco CA"

generate_paycheck_transaction "$user_id" "$account_id" "$item_id"
# ’ "user|account|item|txn-id|3214.50|2024-01-01|Direct Deposit - Acme Corp"

generate_refund_transaction "$user_id" "$account_id" "$item_id"
# ’ "user|account|item|txn-id|129.99|2024-01-12|REFUND - Best Buy"
```

#### Bulk Transaction Generators

```bash
# Generate multiple transactions with realistic distribution
generate_transaction_set "$user_id" "$account_id" "$item_id" 100 30
# Generates 100 transactions over 30 days:
#   60% purchases
#   20% groceries
#   10% paychecks
#   10% refunds
```

### Advanced Functions (Future-Ready)

#### Budget Fixtures (Phase 3)

```bash
# Generate budget for category
generate_category_budget "$user_id" "groceries" 500 "monthly"
# ’ "budget-id|user|Groceries Budget|groceries|500|0.00|monthly|2024-01-01|2024-01-31"

# Generate transactions hitting budget threshold
generate_budget_threshold_transactions "$user_id" "$account_id" "$item_id" 500 0.80 "groceries"
# Generates ~8 grocery transactions totaling $400 (80% of $500 limit)

# Generate refund scenario (spend + refund)
generate_budget_refund_scenario "$user_id" "$account_id" "$item_id" 200 150
# Generates: $200 spend + $150 refund = $50 net impact on budget
```

#### Rule Fixtures (Phase 4)

```bash
# Generate amount threshold rule
generate_amount_threshold_rule "$user_id" 100 "send_notification"
# ’ "rule-id|user|Large Transaction Alert (>$100)|amount|gt|100|send_notification|5|true"

# Generate merchant matching rule
generate_merchant_rule "$user_id" "Starbucks" "auto_categorize" "Coffee"
# ’ "rule-id|user|Auto-Categorize Starbucks|merchant|contains|Starbucks|auto_categorize|8|true|Coffee"

# Generate transaction that triggers rule
generate_rule_trigger_transaction "$user_id" "$account_id" "$item_id" "amount" 100
# ’ Transaction with amount > $100 (triggers threshold rule)
```

### Usage Examples

#### Example 1: Generate Free Tier Dataset (30 Days)

```bash
#!/bin/bash
source lib/fixtures.sh
source lib/db-utils.sh

USER_ID="test-free-user-001"
ACCOUNT_ID="acct-checking-001"
ITEM_ID="item-001"

# Generate 50 transactions over 30 days
transactions=$(generate_transaction_set "$USER_ID" "$ACCOUNT_ID" "$ITEM_ID" 50 30)

# Insert each transaction
while IFS='|' read -r user account item txn_id amount date name; do
    insert_transaction "$user" "$account" "$item" "$txn_id" "$amount" "$date" "$name"
done <<< "$transactions"

echo " Generated 50 transactions for free tier user"
```

#### Example 2: Generate Premium Dataset (730 Days)

```bash
#!/bin/bash
source lib/fixtures.sh
source lib/db-utils.sh

USER_ID="test-premium-user-001"
ACCOUNT_ID="acct-checking-001"
ITEM_ID="item-001"

# Generate 1000 transactions over 730 days (2 years)
transactions=$(generate_transaction_set "$USER_ID" "$ACCOUNT_ID" "$ITEM_ID" 1000 730)

# Insert transactions
while IFS='|' read -r user account item txn_id amount date name; do
    insert_transaction "$user" "$account" "$item" "$txn_id" "$amount" "$date" "$name"
done <<< "$transactions"

echo " Generated 1000 transactions for premium user (2 years)"
```

#### Example 3: Generate Monthly Subscription Pattern

```bash
#!/bin/bash
source lib/fixtures.sh

USER_ID="test-user-001"
ACCOUNT_ID="acct-001"
ITEM_ID="item-001"

# Generate Netflix subscription for 12 months
for month in {0..11}; do
    days_ago=$((month * 30))
    date=$(generate_date_in_past_days $days_ago)

    txn_id="netflix-$(date -d "$date" +%Y%m)"
    amount="-15.99"
    name="Netflix"

    insert_transaction "$USER_ID" "$ACCOUNT_ID" "$ITEM_ID" "$txn_id" "$amount" "$date" "$name"
done

echo " Generated 12 months of Netflix subscription"
```

---

## Dataset Patterns

### Pattern 1: Monthly Spending (Realistic Distribution)

**Use Case**: Scenario 12 - Monthly Spending Patterns

**Distribution**:
- **Groceries**: 25% of transactions (12 txns, $50-$150 each)
- **Restaurants**: 20% of transactions (10 txns, $15-$80 each)
- **Gas**: 15% of transactions (8 txns, $30-$70 each)
- **Shopping**: 20% of transactions (10 txns, $20-$300 each)
- **Subscriptions**: 5% of transactions (3 txns, fixed amounts)
- **Coffee**: 10% of transactions (6 txns, $4-$8 each)
- **Income**: 3% of transactions (2 paychecks, $2000-$4000 each)
- **Utilities**: 2% of transactions (1 txn, $100-$300)

**Key Metrics**:
- Total transactions: ~60
- Total spending: $2,500 - $4,000/month
- Income: $4,000 - $8,000/month
- Net: +$1,500 - +$4,000/month (savings)

**Template Reference**: `templates/transactions.yaml#monthly_spending_free`

---

### Pattern 2: Recurring Subscriptions

**Use Case**: Scenario 13 - Recurring Subscriptions

**Characteristics**:
- **Same merchant name** each month (exact match)
- **Same amount** each month (within $0.01)
- **Same cadence** (~30 days, 28-32 day tolerance)
- **Same payment channel** (usually `online`)

**Example Subscriptions**:
- Netflix: $15.99/month
- Spotify: $10.99/month
- Amazon Prime: $14.99/month
- Planet Fitness: $10.00/month
- Adobe Creative Cloud: $52.99/month

**Detection Algorithm** (for future Phase 3):
```sql
-- Find subscriptions (3+ occurrences, same amount, ~30 day cadence)
SELECT
    name,
    AVG(amount) as avg_amount,
    COUNT(*) as occurrence_count,
    AVG(days_between) as avg_cadence
FROM (
    SELECT
        name,
        amount,
        date,
        LAG(date) OVER (PARTITION BY name ORDER BY date) as prev_date,
        date - LAG(date) OVER (PARTITION BY name ORDER BY date) as days_between
    FROM transactions
    WHERE user_id = 'user-001'
) sub
WHERE days_between BETWEEN 28 AND 32
GROUP BY name
HAVING
    COUNT(*) >= 3 AND
    MAX(amount) - MIN(amount) < 1.00  -- Amount variation < $1
ORDER BY occurrence_count DESC;
```

---

### Pattern 3: Inter-Account Transfers (Net-Zero)

**Use Case**: Scenario 10 - Inter-Account Transfers

**CRITICAL Rule**: Transfer pairs MUST net to $0.00

**Structure**:
```yaml
# Outgoing transfer (Checking ’ Savings)
- external_transaction_id: "transfer-001-out"
  account_id: "checking-001"
  amount: -500.00           # Negative (leaving checking)
  name: "Transfer to Savings"
  date: "2024-01-15"

# Incoming transfer (Savings  Checking)
- external_transaction_id: "transfer-001-in"
  account_id: "savings-001"
  amount: 500.00            # Positive (entering savings)
  name: "Transfer from Checking"
  date: "2024-01-15"        # Same date
```

**Validation**:
```sql
-- Verify net-zero transfers
SELECT SUM(amount) as net_transfer_amount
FROM transactions
WHERE name ILIKE '%transfer%'
  AND user_id = 'user-001';
-- Expected: 0.00
```

**Why This Matters**:
- Transfers should NOT impact net worth
- Transfers should NOT count as income or spending
- Transfers should NOT inflate budget totals

---

### Pattern 4: Pending ’ Posted Transitions

**Use Case**: Scenario 06 - Pending to Posted

**Flow**:
1. **Pending Transaction** (authorized but not settled)
2. **Posted Transaction** (settled, amount may change)

**Example**:
```yaml
# Initial state: Pending
- external_transaction_id: "gas-001"
  amount: -50.00            # Pre-authorization amount
  authorized_amount: -50.00
  date: null                # No date yet (pending)
  authorized_date: "2024-01-15"
  pending: true
  name: "Shell Gas Station"

# After settlement: Posted
- external_transaction_id: "gas-001"  # Same ID
  amount: -47.23            # Actual amount (may differ!)
  authorized_amount: -50.00 # Original auth preserved
  date: "2024-01-17"        # Settlement date (2 days later)
  authorized_date: "2024-01-15"
  pending: false            # No longer pending
  name: "Shell Gas Station"
```

**Key Points**:
- Amount can CHANGE when posted (common for gas, hotels, restaurants)
- `authorized_date` preserved, `date` added on settlement
- `external_transaction_id` remains the same (UPDATE not INSERT)

---

### Pattern 5: Refunds and Credits

**Use Case**: Scenario 09 - Refunds and Credits

**Types of Positive Amounts**:

1. **Full Refund** (100% of original)
```yaml
# Original purchase
- external_transaction_id: "amazon-purchase-001"
  amount: -129.99
  date: "2024-01-10"
  name: "Amazon.com - Laptop Stand"

# Full refund (same amount, positive)
- external_transaction_id: "amazon-refund-001"
  amount: 129.99            # Positive!
  date: "2024-01-15"
  name: "Amazon.com - Refund"
```

2. **Partial Refund** (<100% of original)
```yaml
# Original purchase
- external_transaction_id: "target-purchase-001"
  amount: -85.00
  date: "2024-01-12"
  name: "Target"

# Partial refund (item on sale, price adjustment)
- external_transaction_id: "target-refund-001"
  amount: 12.75             # Partial refund
  date: "2024-01-14"
  name: "Target - Price Adjustment"
```

3. **Cashback** (no original purchase)
```yaml
- external_transaction_id: "cashback-001"
  amount: 25.00
  date: "2024-01-20"
  name: "Chase Cashback Rewards"
  personal_finance_category:
    primary: "INCOME"
    detailed: "INCOME_OTHER_INCOME"
```

4. **Reimbursement**
```yaml
- external_transaction_id: "reimburse-001"
  amount: 50.00
  date: "2024-01-25"
  name: "Employer Expense Reimbursement"
  personal_finance_category:
    primary: "INCOME"
    detailed: "INCOME_OTHER_INCOME"
```

**Detection**: All have `amount > 0` (positive)

---

### Pattern 6: Large Purchases (Fraud Detection)

**Use Case**: Scenario 14 - Large Purchases

**Threshold**: Transactions > $500

**Common Large Purchases**:
- **Rent**: $1,500 - $3,000/month
- **Tuition**: $2,000 - $10,000/semester
- **Flights**: $300 - $2,000
- **Electronics**: $500 - $3,000 (laptops, TVs, phones)
- **Furniture**: $500 - $5,000
- **Medical Bills**: $500 - $5,000
- **Auto Repairs**: $500 - $3,000

**Business Value**:
- **Fraud Alerts**: Unusual large transaction ’ notify user
- **Savings Goals**: "You spent $1,899 on laptop, want to create savings goal?"
- **Budget Shocks**: "Warning: Large purchase will exceed monthly budget"

**SQL Query**:
```sql
-- Find large purchases (>$500)
SELECT
    date,
    name,
    ABS(amount) as amount,
    personal_finance_category->>'primary' as category
FROM transactions
WHERE user_id = 'user-001'
  AND ABS(amount) > 500
  AND amount < 0  -- Exclude income
ORDER BY ABS(amount) DESC
LIMIT 10;
```

---

### Pattern 7: Micro-Transactions ("Latte Factor")

**Use Case**: Scenario 17 - Daily Micro-Transactions

**Definition**: Transactions < $10 (typically $3-$8)

**Common Examples**:
- **Coffee**: $4-$8 (Starbucks, Dunkin')
- **Public Transit**: $2-$5 (Metro, bus)
- **Snacks**: $3-$6 (convenience store, vending machine)
- **Fast Food**: $5-$10 (quick lunch)

**The "Latte Factor"**:
- $5 coffee × 20 workdays/month = **$100/month**
- $100/month × 12 months = **$1,200/year**
- Over 10 years: **$12,000+** (with compound interest)

**Business Value**:
- Spending awareness: "You've spent $127 on coffee this month"
- Savings suggestions: "Save $50/month by making coffee at home"

**SQL Query**:
```sql
-- Calculate monthly "latte factor"
SELECT
    COUNT(*) as micro_transaction_count,
    SUM(ABS(amount)) as total_spent,
    AVG(ABS(amount)) as avg_amount,
    SUM(ABS(amount)) / 30 * 365 as projected_annual
FROM transactions
WHERE user_id = 'user-001'
  AND ABS(amount) < 10
  AND amount < 0
  AND date >= CURRENT_DATE - INTERVAL '30 days';
```

---

### Pattern 8: Seasonal Spending

**Use Case**: Scenario 18 - Seasonal Spending

**Holiday Spending Patterns**:

| Period | Spending Multiplier | Example |
|--------|---------------------|---------|
| Normal Week | 1.0× (baseline) | $400/week |
| Pre-Holiday | 1.5× | $600/week (shopping starts) |
| Holiday Week | 2.5× | $1,000/week (gifts, travel) |
| Post-Holiday | 0.7× | $280/week (recovery) |

**Major Spending Holidays**:
- **Thanksgiving** (Nov): Travel, groceries, gifts (1.5-2× normal)
- **Black Friday** (Nov): Shopping surge (2-3× normal)
- **Christmas** (Dec): Gifts, travel, food (2-3× normal)
- **Back to School** (Aug-Sep): Clothing, supplies (1.5-2× normal)
- **Tax Season** (Apr): Large payments, accounting fees (variable)

**Week-over-Week Comparison**:
```sql
-- Calculate week-over-week spending changes
SELECT
    DATE_TRUNC('week', date) as week,
    SUM(ABS(amount)) as weekly_spending,
    LAG(SUM(ABS(amount))) OVER (ORDER BY DATE_TRUNC('week', date)) as prev_week_spending,
    (SUM(ABS(amount)) / LAG(SUM(ABS(amount))) OVER (ORDER BY DATE_TRUNC('week', date))) as multiplier
FROM transactions
WHERE user_id = 'user-001'
  AND amount < 0  -- Spending only
GROUP BY DATE_TRUNC('week', date)
ORDER BY week DESC;
```

---

## Best Practices

### 1. **Choose Templates vs. Fixtures**

| Scenario | Use Templates (YAML) | Use Fixtures (Bash) |
|----------|---------------------|---------------------|
| Static test cases |  Yes | L No |
| Need exact control |  Yes | L No |
| Rapid prototyping |  Yes | L No |
| Large datasets (>100 txns) | L No |  Yes |
| Random/varied data | L No |  Yes |
| Automated seeding | L No |  Yes |

**Rule of Thumb**: Templates for scenarios, fixtures for bulk seeding.

### 2. **Realistic Data Distribution**

L **Unrealistic**:
```yaml
# Every transaction is exactly $50
transactions:
  - amount: -50.00
  - amount: -50.00
  - amount: -50.00
```

 **Realistic**:
```yaml
# Varied amounts, realistic merchants, dates
transactions:
  - <<: *starbucks
    amount: -4.85
    date: "2024-01-15"
  - <<: *whole_foods
    amount: -127.43
    date: "2024-01-16"
  - <<: *shell
    amount: -55.20
    date: "2024-01-17"
```

### 3. **Merchant Diversity**

**Guideline**: Use e20 unique merchants for monthly spending

L **Unrealistic** (only 3 merchants):
```yaml
# All groceries from Whole Foods
# All gas from Shell
# All coffee from Starbucks
```

 **Realistic** (varied merchants):
```yaml
# Groceries: Whole Foods, Safeway, Trader Joe's, Target
# Gas: Shell, Chevron, BP
# Coffee: Starbucks, Dunkin', Peet's, Local Cafe
```

### 4. **Category Balance**

**Typical Monthly Spending Breakdown**:
- Housing (rent/mortgage): 30%
- Food (groceries + restaurants): 15%
- Transportation (gas + transit): 10%
- Utilities: 5%
- Shopping: 15%
- Entertainment: 10%
- Subscriptions: 5%
- Other: 10%

### 5. **Date Distribution**

L **Unrealistic** (all on same day):
```yaml
- date: "2024-01-15"  # 50 transactions
- date: "2024-01-15"
- date: "2024-01-15"
```

 **Realistic** (spread across month):
```yaml
- date: "2024-01-03"
- date: "2024-01-05"
- date: "2024-01-08"
- date: "2024-01-12"
# ... distributed across 30 days
```

### 6. **Amount Realism**

**Use typical ranges per category**:
- Coffee: $4-$8 (not $50)
- Groceries: $50-$200 (not $10)
- Rent: $1,500-$3,000 (not $100)
- Gas: $30-$70 (not $200)

---

## Common Pitfalls

### Pitfall 1: Forgetting Net-Zero Transfers

L **Wrong** (net worth decreases by $500):
```yaml
- external_transaction_id: "transfer-out"
  amount: -500.00  # Leaving checking
  # Missing the +500.00 in savings!
```

 **Correct** (net worth unchanged):
```yaml
- external_transaction_id: "transfer-001-out"
  account_id: "checking-001"
  amount: -500.00

- external_transaction_id: "transfer-001-in"
  account_id: "savings-001"
  amount: 500.00  # Net = $0
```

### Pitfall 2: Refund Without Original Purchase

L **Confusing** (refund appears out of nowhere):
```yaml
- external_transaction_id: "refund-001"
  amount: 129.99  # Where's the original purchase?
```

 **Clear** (refund references original):
```yaml
# Original purchase (in existing_transactions)
- external_transaction_id: "amazon-purchase-001"
  amount: -129.99
  date: "2024-01-10"

# Refund (in expected transactions after webhook)
- external_transaction_id: "amazon-refund-001"
  amount: 129.99
  date: "2024-01-15"
  name: "Amazon.com - Refund for Order #123"
```

### Pitfall 3: Subscription Amount Drift

L **Unrealistic** (subscription amount changes each month):
```yaml
- name: "Netflix"
  amount: -15.99  # January
- name: "Netflix"
  amount: -16.50  # February (why did it change?)
- name: "Netflix"
  amount: -14.99  # March (changed again?)
```

 **Realistic** (subscription = fixed amount):
```yaml
- name: "Netflix"
  amount: -15.99  # January
- name: "Netflix"
  amount: -15.99  # February (same)
- name: "Netflix"
  amount: -15.99  # March (same)
```

### Pitfall 4: Ignoring Business Days

L **Unrealistic** (paycheck on Sunday):
```yaml
- name: "Direct Deposit"
  date: "2024-01-14"  # Sunday (banks closed!)
```

 **Realistic** (paycheck on Friday):
```yaml
- name: "Direct Deposit"
  date: "2024-01-12"  # Friday (typical payday)
```

---

## Quick Reference

### Scenario Type ’ Dataset Pattern Mapping

| Scenario Type | Primary Template | Fixtures Function | Key Pattern |
|--------------|------------------|-------------------|-------------|
| First Sync (Free) | `monthly_spending_free` | `generate_transaction_set` | 30 days, 50 txns |
| First Sync (Premium) | `historical_premium` | `generate_transaction_set` | 730 days, 1000 txns |
| Pending ’ Posted | `pending_transaction` | N/A | Amount changes |
| Refunds | `refund_credit` | `generate_refund_transaction` | Positive amounts |
| Transfers | N/A | `generate_transfer_pairs` | Net-zero |
| Recurring | `subscription` | N/A | Same amount/cadence |
| Large Purchases | N/A | `generate_amount` | >$500 |
| Micro-Transactions | N/A | Coffee/transit generators | <$10 |
| Monthly Patterns | `monthly_spending_free` | Multiple generators | Category distribution |
| Merchant Diversity | `merchants.yaml` | `generate_merchant_name` | 20+ merchants |

---

## Next Steps

1. **Browse Templates**: Explore `templates/` directory for reusable anchors
2. **Read Scenarios**: See `scenarios/` for complete examples (01-18)
3. **Experiment with Fixtures**: Run `lib/fixtures.sh` functions in `bash` shell
4. **Create Custom Scenario**: Combine templates + custom data in new YAML file
5. **Run Scenarios**: Use `./run-scenario.sh scenarios/your-scenario.yaml`

---

**Questions? See**:
- `README.md` - Framework overview and usage
- `HOW-IT-WORKS.md` - Technical architecture
- `PROGRESS.md` - Implementation status
- `templates/` - All available templates
- `lib/fixtures.sh` - All programmatic generators
