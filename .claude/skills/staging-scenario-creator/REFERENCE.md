# Staging Scenario Schema Reference

Complete schema documentation and examples for BoostedBudget staging data populator scenarios.

## Table of Contents

1. [Complete Schema](#complete-schema)
2. [Field Definitions](#field-definitions)
3. [Account Types](#account-types)
4. [Subscription Tiers](#subscription-tiers)
5. [Transaction Modes](#transaction-modes)
6. [Assertion Operators](#assertion-operators)
7. [Complete Examples](#complete-examples)

---

## Complete Schema

```yaml
version: "2.0"

metadata:
  name: string                    # Required: Scenario display name
  description: string             # Required: Purpose and use case
  tags: [string, ...]             # Optional: Categorization tags

setup:
  user:
    email: string                 # Email address (for mode: new) or existing user
    tier: "free" | "premium" | "premium_plus"  # Subscription tier

  linked_items:
    - institution: string         # Bank/institution name
    # ... more items

  accounts:
    - item_index: integer         # Linked item index (0-based)
      type: string                # Account type (see Account Types)
      name: string                # Display name
      balance: number             # Current balance
    # ... more accounts

transactions:
  mode: "auto" | "manual"         # Generation mode

  # For auto mode:
  count_per_account: integer      # Transactions per account

  # For manual mode:
  list:
    - account_index: integer      # Account index (0-based)
      amount: number              # Transaction amount (- expense, + income)
      date: string                # Date (YYYY-MM-DD, "today", "yesterday")
      name: string                # Merchant/description
      category: string            # Category name or JSON array
      pending: boolean            # Pending status
      user_notes: string          # Optional user notes
    # ... more transactions

assertions:
  database:
    transactions_count: string    # e.g., ">= 100", "= 45"
    account_count: string         # e.g., "= 3"
    user_tier: string             # e.g., "= free"
    total_balance: string         # Optional: balance validation
```

---

## Field Definitions

### Metadata Section

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `name` | string | Yes | Human-readable scenario name | "UI Testing - Pagination" |
| `description` | string | Yes | Purpose and use case (1-2 sentences) | "Test pagination with 1000 transactions" |
| `tags` | array | No | Categorization tags | ["ui-testing", "pagination"] |

### User Section

| Field | Type | Required | Description | Values |
|-------|------|----------|-------------|--------|
| `email` | string | Yes | User email address | "test@example.com" |
| `tier` | string | Yes | Subscription tier | "free", "premium", "premium_plus" |

### Linked Items Section

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `institution` | string | Yes | Bank/institution name | "Chase", "Wells Fargo" |

### Accounts Section

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `item_index` | integer | Yes | Linked item reference (0-based) | 0, 1, 2 |
| `type` | string | Yes | Account type (see Account Types) | "checking", "savings" |
| `name` | string | Yes | Display name | "Chase Total Checking" |
| `balance` | number | Yes | Current balance | 2847.53, -1247.89 |

### Transactions Section

| Field | Type | Required | Description | Values |
|-------|------|----------|-------------|--------|
| `mode` | string | Yes | Generation mode | "auto", "manual" |
| `count_per_account` | integer | Auto mode | Transactions per account | 15, 100, 200 |
| `list` | array | Manual mode | Specific transactions | See manual transaction fields |

**Manual Transaction Fields:**

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `account_index` | integer | Yes | Account reference (0-based) | 0, 1, 2 |
| `amount` | number | Yes | Amount (- expense, + income) | -25.50, 1000.00 |
| `date` | string | Yes | Date (YYYY-MM-DD or relative) | "2025-11-12", "today" |
| `name` | string | Yes | Merchant/description | "Starbucks", "Salary Deposit" |
| `category` | string | No | Category | "Food and Drink", '["Shopping"]' |
| `pending` | boolean | No | Pending status (default: false) | true, false |
| `user_notes` | string | No | User notes | "Business expense" |

### Assertions Section

| Field | Type | Required | Description | Example |
|-------|------|----------|-------------|---------|
| `transactions_count` | string | No | Transaction count validation | ">= 45", "= 100" |
| `account_count` | string | No | Account count validation | "= 3", ">= 5" |
| `user_tier` | string | No | Tier validation | "= free", "= premium" |
| `total_balance` | string | No | Balance validation | ">= 1000" |

---

## Account Types

### User-Friendly Types (Use in YAML)

| YAML Type | Maps To | Description | Typical Balance |
|-----------|---------|-------------|-----------------|
| `checking` | depository | Checking account | $1,000 - $10,000 |
| `savings` | depository | Savings account | $5,000 - $50,000 |
| `credit_card` | credit | Credit card | -$500 to -$5,000 |
| `loan` | loan | Loan (student, auto, personal) | -$10,000 to -$100,000 |
| `investment` | investment | Brokerage, 401k, IRA | $10,000 - $500,000 |

### Database Types (Backend Converts)

- `depository` - Bank accounts (checking, savings)
- `credit` - Credit accounts
- `loan` - Loan accounts
- `investment` - Investment accounts
- `other` - Other account types

---

## Subscription Tiers

| Tier | Account Limit | Historical Days | Sync Frequency | Use Case |
|------|---------------|-----------------|----------------|----------|
| `free` | 3 | 30 | 1x/day | Basic users, demos |
| `premium` | 30 | 365 | 2x/day | Power users |
| `premium_plus` | Unlimited | 730 | 6x/day | Premium users |

**Historical Days** = How far back transactions are generated in auto mode

---

## Transaction Modes

### Auto Mode

**Best for:** Large datasets, UI testing, performance testing

**Characteristics:**
- Generates random realistic transactions
- ~90% expenses, ~10% income
- ~10% pending transactions
- Date range based on tier (30/365/730 days)
- Random merchants from predefined pools
- Random categories
- Random amounts (expenses: $5-$200, income: $2000-$5000)

**Example:**
```yaml
transactions:
  mode: "auto"
  count_per_account: 100  # 100 transactions per account
```

### Manual Mode

**Best for:** Edge cases, specific testing scenarios, controlled data

**Characteristics:**
- Define each transaction explicitly
- Full control over amount, date, category, pending status
- Can add user notes
- Perfect for testing refunds, pending transitions, specific categories

**Example:**
```yaml
transactions:
  mode: "manual"
  list:
    - account_index: 0
      amount: -45.32
      date: "2025-11-10"
      name: "Starbucks"
      category: "Food and Drink"
      pending: false

    - account_index: 0
      amount: 3500.00
      date: "2025-11-01"
      name: "Direct Deposit - Payroll"
      category: "Income"
      pending: false
```

---

## Assertion Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `=` | Exact match | `"= 45"` - exactly 45 |
| `>=` | Greater than or equal | `">= 100"` - 100 or more |
| `<=` | Less than or equal | `"<= 3"` - 3 or fewer |
| `>` | Greater than | `"> 0"` - more than 0 |
| `<` | Less than | `"< 1000"` - less than 1000 |

**Format:** Always quote the operator + value as a string

**Examples:**
```yaml
assertions:
  database:
    transactions_count: ">= 45"      # At least 45 transactions
    account_count: "= 3"              # Exactly 3 accounts
    user_tier: "= free"               # Tier must be free
    total_balance: ">= 1000"          # Total balance >= $1000
```

---

## Complete Examples

### Example 1: Simple Demo - Free Tier

```yaml
version: "2.0"

metadata:
  name: "Demo Data - Free Tier User"
  description: "Create realistic demo data for free tier user with 3 accounts"
  tags: ["demo", "free-tier", "ui-testing"]

setup:
  user:
    email: "demo-free@example.com"
    tier: "free"

  linked_items:
    - institution: "Chase"

  accounts:
    - item_index: 0
      type: "checking"
      name: "Chase Total Checking"
      balance: 2847.53

    - item_index: 0
      type: "savings"
      name: "Chase Savings"
      balance: 5230.00

    - item_index: 0
      type: "credit_card"
      name: "Chase Freedom"
      balance: -1247.89

transactions:
  mode: "auto"
  count_per_account: 15  # 45 total transactions

assertions:
  database:
    transactions_count: ">= 45"
    account_count: "= 3"
    user_tier: "= free"
```

### Example 2: UI Testing - Multi-Account

```yaml
version: "2.0"

metadata:
  name: "UI Testing - Multi-Account Pagination"
  description: "Create many accounts and transactions for testing pagination and performance"
  tags: ["ui-testing", "pagination", "performance", "premium"]

setup:
  user:
    email: "test-pagination@example.com"
    tier: "premium"

  linked_items:
    - institution: "Chase"
    - institution: "Wells Fargo"

  accounts:
    - item_index: 0
      type: "checking"
      name: "Checking Account 1"
      balance: 3247.82

    - item_index: 0
      type: "savings"
      name: "Savings Account 1"
      balance: 12450.00

    - item_index: 0
      type: "credit_card"
      name: "Credit Card 1"
      balance: -2847.23

    - item_index: 0
      type: "checking"
      name: "Business Checking"
      balance: 8932.41

    - item_index: 1
      type: "checking"
      name: "Checking Account 2"
      balance: 1834.92

    - item_index: 1
      type: "savings"
      name: "Emergency Fund"
      balance: 25000.00

    - item_index: 1
      type: "credit_card"
      name: "Credit Card 2"
      balance: -1293.45

    - item_index: 1
      type: "investment"
      name: "Brokerage Account"
      balance: 45230.18

    - item_index: 1
      type: "loan"
      name: "Student Loan"
      balance: -28450.00

    - item_index: 1
      type: "loan"
      name: "Auto Loan"
      balance: -15320.00

transactions:
  mode: "auto"
  count_per_account: 100  # 1000 total transactions

assertions:
  database:
    transactions_count: ">= 1000"
    account_count: "= 10"
```

### Example 3: Edge Cases - Pending & Refunds

```yaml
version: "2.0"

metadata:
  name: "Edge Cases - Pending Transactions & Refunds"
  description: "Test edge cases including pending transactions, refunds, and user notes"
  tags: ["edge-case", "pending", "refunds", "user-notes"]

setup:
  user:
    email: "test-edge-cases@example.com"
    tier: "free"

  linked_items:
    - institution: "Chase"

  accounts:
    - item_index: 0
      type: "checking"
      name: "Checking"
      balance: 1500.00

    - item_index: 0
      type: "credit_card"
      name: "Credit Card"
      balance: -500.00

transactions:
  mode: "manual"
  list:
    # Pending purchase
    - account_index: 1
      amount: -89.99
      date: "today"
      name: "Amazon.com"
      category: "Shopping"
      pending: true
      user_notes: "Pending charge"

    # Refund (positive amount on credit card)
    - account_index: 1
      amount: 45.32
      date: "yesterday"
      name: "Starbucks Refund"
      category: "Food and Drink"
      pending: false
      user_notes: "Refund for wrong order"

    # Large pending deposit
    - account_index: 0
      amount: 3500.00
      date: "today"
      name: "Direct Deposit - Payroll"
      category: "Income"
      pending: true

    # Completed transaction
    - account_index: 0
      amount: -125.00
      date: "2025-11-10"
      name: "Whole Foods Market"
      category: "Groceries"
      pending: false

    # Another refund
    - account_index: 0
      amount: 25.00
      date: "2025-11-09"
      name: "Target Refund"
      category: "Shopping"
      pending: false
      user_notes: "Returned defective item"

assertions:
  database:
    transactions_count: ">= 5"
    account_count: "= 2"
```

### Example 4: Tier Limit Testing

```yaml
version: "2.0"

metadata:
  name: "Tier Limit Testing - Free Tier Maximum"
  description: "Test free tier with exactly 3 accounts (maximum allowed)"
  tags: ["tier-limit", "free-tier", "validation"]

setup:
  user:
    email: "test-free-limit@example.com"
    tier: "free"

  linked_items:
    - institution: "Chase"

  accounts:
    - item_index: 0
      type: "checking"
      name: "Checking"
      balance: 1000.00

    - item_index: 0
      type: "savings"
      name: "Savings"
      balance: 5000.00

    - item_index: 0
      type: "credit_card"
      name: "Credit Card"
      balance: -500.00

transactions:
  mode: "auto"
  count_per_account: 10  # 30 total

assertions:
  database:
    transactions_count: ">= 30"
    account_count: "= 3"
    user_tier: "= free"
```

---

## Common Patterns

### Pattern: Basic Demo Scenario
- **Tier:** free
- **Accounts:** 3 (checking, savings, credit_card)
- **Transactions:** auto, 10-20 per account
- **Use:** General demos, basic UI testing

### Pattern: Performance Testing
- **Tier:** premium or premium_plus
- **Accounts:** 10-20
- **Transactions:** auto, 100-200 per account (1000-4000 total)
- **Use:** Pagination, performance, stress testing

### Pattern: Edge Case Testing
- **Tier:** Any
- **Accounts:** 2-3
- **Transactions:** manual, 5-10 specific scenarios
- **Use:** Pending, refunds, user notes, special categories

### Pattern: Tier Limit Validation
- **Tier:** free (3 accounts), premium (30 accounts)
- **Accounts:** Exactly at limit
- **Transactions:** auto, small count
- **Use:** Tier enforcement, upgrade prompts

---

## Tips & Best Practices

### Account Configuration

1. **Realistic balances:**
   - Checking: $1,000 - $10,000
   - Savings: $5,000 - $50,000
   - Credit cards: -$500 to -$5,000 (negative)
   - Loans: -$10,000 to -$100,000 (negative)
   - Investment: $10,000 - $500,000

2. **Account naming:**
   - Be specific: "Chase Freedom Unlimited" not "Card 1"
   - Use real bank names for authenticity
   - Include account type in name: "Business Checking", "Emergency Savings"

### Transaction Configuration

1. **Auto mode for volume:**
   - Use when you need 50+ transactions
   - Perfect for pagination testing
   - Realistic mix of categories and amounts

2. **Manual mode for precision:**
   - Use for specific test cases
   - Edge cases (refunds, pending)
   - Scenarios requiring exact data

3. **Date selection:**
   - Use "today" for current transactions
   - Use "yesterday" for recent history
   - Use specific dates (YYYY-MM-DD) for historical data

### Assertions

1. **Always validate counts:**
   - Transaction count ensures data was generated
   - Account count ensures setup succeeded

2. **Use >= for auto mode:**
   - Random generation may vary slightly
   - `">= N"` is safer than `"= N"`

3. **Validate tier:**
   - Ensures tier was set correctly
   - Critical for tier limit testing

---

## Troubleshooting

### Common Issues

**Issue:** "Invalid account type"
- **Fix:** Use user-friendly types: checking, savings, credit_card, loan, investment

**Issue:** "Tier limit exceeded"
- **Fix:** Free tier max 3 accounts, premium max 30

**Issue:** "Invalid item_index"
- **Fix:** item_index is 0-based, must reference existing linked_items

**Issue:** "Invalid account_index in transaction"
- **Fix:** account_index is 0-based, must reference existing accounts

**Issue:** "Transaction mode mismatch"
- **Fix:** Use either `count_per_account` (auto) OR `list` (manual), not both

---

For usage instructions, see the main Skill.md file.
