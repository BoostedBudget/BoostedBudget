# Scenario Schema Guide

Complete reference for creating YAML scenario files. Optimized for AI code generation (Claude Code).

## Quick Template (Copy-Paste)

```yaml
version: "2.0"
metadata:
  name: "Descriptive Scenario Name"
  description: "What this scenario tests or demonstrates"
  tags: ["tag1", "tag2"]

setup:
  user:
    email: "unique-email@example.com"
    tier: "free"  # or premium_lite, premium, premium_plus

  linked_items:
    - institution: "Chase"  # or "Bank of America", "Wells Fargo", etc.

  accounts:
    - item_index: 0  # References linked_items array index
      type: "checking"  # or savings, credit_card, investment, loan
      name: "Account Name"
      balance: 1000.00

  transactions:
    count_per_account: 20  # Auto-generate 20 random realistic transactions

assertions:
  - type: "transaction_count"
    operator: ">="
    value: 20

  - type: "account_count"
    operator: "="
    value: 1
```

## Field Reference

### Metadata

```yaml
metadata:
  name: "Short descriptive name"
  description: "Longer explanation of what this tests (optional)"
  tags: ["demo", "testing", "tier:free", "edge-cases"]
```

**Tags** (suggested conventions):
- Use case: `demo`, `testing`, `ui-testing`, `performance`
- Tier: `free-tier`, `premium`, `tier-limits`
- Features: `pagination`, `edge-cases`, `pending`, `refunds`

### User Configuration

```yaml
setup:
  user:
    mode: "new"  # OPTIONAL - "existing" or "new" (default: "new")
    email: "unique-email@example.com"  # REQUIRED - must be unique
    tier: "free"  # REQUIRED for new users, OPTIONAL for existing users
    clerk_user_id: "user_2abc123..."  # OPTIONAL - for validation only
```

**User Modes:**
- `new` - Create a new fake user with fake Clerk ID (default)
  - **Frontend will NOT work** - cannot log in with fake Clerk users
  - Use for backend testing, API testing, SQL queries
  - Script generates fake clerk_user_id automatically

- `existing` - Use a real Clerk user who has logged into staging
  - **Frontend WILL work** - user can log in and see populated data!
  - User must exist in database (must have logged into staging.boostedbudget.com first)
  - Script preserves user account during cleanup (only deletes data)
  - Optionally updates tier if specified
  - Use `helpers/list-users.sh` to find existing users

**Tiers:**
- `free` - 3 accounts max, 1 rule, 30-day transaction history
- `premium_lite` - 15 accounts, 5 rules, 365-day history
- `premium` - 30 accounts, 15 rules, 730-day history
- `premium_plus` - Unlimited accounts/rules, 730-day history

**Email Conventions:**
- Test data: `test-<scenario-name>@example.com`
- Demo data: `demo-<tier>@example.com`
- Edge cases: `test-edge-<case>@example.com`

**Example - New User (Fake Data):**
```yaml
setup:
  user:
    mode: "new"  # or omit (defaults to "new")
    email: "test-new-user@example.com"
    tier: "free"
```

**Example - Existing User (Frontend Testing):**
```yaml
setup:
  user:
    mode: "existing"  # REQUIRED for real Clerk users
    email: "your-email@example.com"  # Must match Clerk login
    tier: "premium"  # Optional - upgrades user's tier
    # clerk_user_id: "user_2abc123..."  # Optional validation
```

### Linked Items

```yaml
linked_items:
  - institution: "Chase"
  - institution: "Bank of America"
  - institution: "Wells Fargo"
```

**Common Institutions:**
- Chase, Bank of America, Wells Fargo, Citibank
- Capital One, US Bank, PNC Bank, TD Bank
- American Express, Discover

### Accounts

```yaml
accounts:
  - item_index: 0  # REQUIRED - references linked_items[0]
    type: "checking"  # REQUIRED
    name: "Account Name"  # REQUIRED
    balance: 1000.00  # REQUIRED
```

**Account Types:**
- `checking` - Standard checking account (positive balance)
- `savings` - Savings account (positive balance)
- `credit_card` - Credit card (negative balance = amount owed)
- `investment` - Brokerage/investment account (positive balance)
- `loan` - Mortgage, student loan, auto loan (negative balance)

**Balance Guidelines:**
- Checking: $500 - $10,000
- Savings: $1,000 - $50,000
- Credit cards: -$200 to -$5,000 (negative)
- Investment: $5,000 - $100,000
- Loans: -$5,000 to -$500,000 (negative)

### Transactions - Auto-Generated

**Simple (Recommended):**
```yaml
transactions:
  count_per_account: 20  # Generates 20 realistic transactions per account
```

**Features:**
- Realistic merchant names (50+ options: Starbucks, Amazon, Target, etc.)
- Proper categories (Food, Shopping, Transportation, Entertainment, etc.)
- Randomized amounts with cents ($5.00 - $200.00 expenses)
- Random dates within tier's historical limit (30/365/730 days)
- 10% income transactions ($2000-$5000)
- 10% pending transactions
- 90% expenses, 10% income ratio

### Transactions - Manual Definition

**For Testing Edge Cases:**
```yaml
transactions:
  - account_index: 0  # REQUIRED - references accounts[0]
    amount: -42.50  # REQUIRED - negative = expense, positive = income
    date: "2025-11-01"  # REQUIRED - YYYY-MM-DD format
    name: "Starbucks"  # REQUIRED - merchant name
    category: ["Food and Drink", "Coffee Shop"]  # Optional
    pending: false  # Optional - true for pending transactions
    user_notes: "Morning coffee with client"  # Optional - test notes preservation
```

**Manual Transaction Use Cases:**
1. **Pending Transactions:** `pending: true`
2. **Refunds:** Positive amount in expense category (e.g., `amount: 50.00, name: "Amazon Refund"`)
3. **User Notes:** Test notes preservation across syncs
4. **Specific Dates/Amounts:** Edge case testing

### Webhooks (Optional - Balance Snapshots)

**NEW in Issue #353:** Fire Plaid webhooks after data insertion to create balance snapshots.

```yaml
webhooks:
  fire_after_setup: true  # Fire webhook after data insertion
  webhook_code: "DEFAULT_UPDATE"  # or INITIAL_UPDATE, HISTORICAL_UPDATE
  wait_for_processing: true  # Wait for async processing to complete
  timeout_seconds: 30  # Maximum wait time (default: 30)
```

**Webhook Codes:**
- `DEFAULT_UPDATE` - Standard incremental sync (recommended for testing)
- `INITIAL_UPDATE` - First sync with historical depth based on tier
- `HISTORICAL_UPDATE` - Backfill old transactions
- `TRANSACTIONS_REMOVED` - Retroactive deletions

**Requirements:**
- `AUTH_TOKEN` environment variable must be set with valid Clerk JWT
- Admin webhook endpoint must be deployed to staging
- Webhook processing creates balance snapshots for all accounts

**Why Use Webhooks:**
- ✅ Creates balance snapshots (for dashboard testing)
- ✅ Tests the full webhook processing pipeline
- ✅ Matches production behavior exactly
- ❌ Slower (adds 5-30 seconds per scenario)
- ❌ Requires Clerk authentication

**Example:**
```yaml
metadata:
  name: "Dashboard Balance Test"
  tags: ["dashboard", "balance", "snapshots"]

setup:
  user:
    email: "test-dashboard@example.com"
    tier: "premium"
  accounts:
    - {item_index: 0, type: "checking", name: "Checking", balance: 5000}

webhooks:
  fire_after_setup: true
  webhook_code: "DEFAULT_UPDATE"
  wait_for_processing: true

assertions:
  - type: "balance_snapshot_count"
    operator: "="
    value: 1  # One snapshot created for today
```

### Assertions

**Transaction Count:**
```yaml
- type: "transaction_count"
  operator: ">="  # or "=", "<=", ">", "<"
  value: 50
```

**Account Count:**
```yaml
- type: "account_count"
  operator: "="
  value: 3
```

**User Tier:**
```yaml
- type: "user_tier"
  expected: "free"  # or premium_lite, premium, premium_plus
```

**Balance Sum:**
```yaml
- type: "balance_sum"
  operator: ">="
  value: 10000.00
```

## Common Patterns

### Free Tier User (QA Testing)

```yaml
metadata: {name: "QA Testing - Free Tier", tags: ["demo", "free-tier"]}
setup:
  user: {email: "demo-free@example.com", tier: "free"}
  linked_items: [{institution: "Chase"}]
  accounts:
    - {item_index: 0, type: "checking", name: "Checking", balance: 1500}
    - {item_index: 0, type: "savings", name: "Savings", balance: 3000}
    - {item_index: 0, type: "credit_card", name: "Credit Card", balance: -500}
  transactions: {count_per_account: 10}
assertions:
  - {type: "account_count", operator: "=", value: 3}
  - {type: "transaction_count", operator: ">=", value: 30}
```

### Premium User (Demo Data)

```yaml
metadata: {name: "Demo - Premium User", tags: ["demo", "premium"]}
setup:
  user: {email: "demo-premium@example.com", tier: "premium"}
  linked_items:
    - {institution: "Chase"}
    - {institution: "Bank of America"}
  accounts:  # 5-10 accounts across multiple institutions
    - {item_index: 0, type: "checking", name: "Chase Checking", balance: 5000}
    - {item_index: 0, type: "savings", name: "Chase Savings", balance: 20000}
    - {item_index: 1, type: "investment", name: "BofA Investment", balance: 75000}
  transactions: {count_per_account: 50}  # 2-year history (730 days)
```

### Edge Cases (Manual Transactions)

```yaml
metadata: {name: "Edge Cases", tags: ["testing", "edge-cases"]}
setup:
  user: {email: "test-edge-cases@example.com", tier: "premium_lite"}
  accounts: [{item_index: 0, type: "checking", name: "Checking", balance: 2000}]
  transactions:
    - {account_index: 0, amount: -100, date: "2025-11-10", name: "Amazon", pending: true}
    - {account_index: 0, amount: 50, date: "2025-11-08", name: "Amazon Refund", pending: false}
    - {account_index: 0, amount: -25, date: "2025-11-05", name: "Starbucks", user_notes: "Test note", pending: false}
```

### Pagination Testing (Many Transactions)

```yaml
metadata: {name: "Pagination Test", tags: ["performance", "pagination"]}
setup:
  user: {email: "test-pagination@example.com", tier: "premium"}
  accounts: [{item_index: 0, type: "checking", name: "Checking", balance: 5000}]
  transactions: {count_per_account: 500}  # Test pagination (500 transactions)
assertions:
  - {type: "transaction_count", operator: ">=", value: 500}
```

## Examples by Use Case

### 1. QA Manual Testing
**Goal:** Populate UI with realistic data for QA team
**Pattern:** Free/Premium tier, 3-10 accounts, 20-50 transactions per account
**Example:** `scenarios/01-demo-data-free-user.yaml`

### 2. Demo/Screenshots
**Goal:** Beautiful data for product demos
**Pattern:** Premium tier, diverse account types, balanced amounts
**Example:** `scenarios/02-demo-data-premium-user.yaml`

### 3. Performance/Load Testing
**Goal:** Test UI with many accounts/transactions
**Pattern:** Premium tier, 10+ accounts, 100+ transactions per account
**Example:** `scenarios/03-ui-testing-multi-account.yaml`

### 4. Tier Limit Testing
**Goal:** Verify tier limits enforced correctly
**Pattern:** Free tier with exactly 3 accounts (max)
**Example:** `scenarios/04-tier-limit-testing.yaml`

### 5. Edge Case Testing
**Goal:** Test specific edge cases (pending, refunds, notes)
**Pattern:** Manual transaction definitions
**Example:** `scenarios/05-edge-cases-pending-refunds.yaml`

## Tips for Claude Code

When generating scenarios:

1. **Always use unique emails** - Prevents conflicts with existing test data
2. **Match tier to use case** - Free for QA, Premium for demos
3. **Use `count_per_account` for speed** - Auto-generation is faster than manual
4. **Manual transactions for edge cases only** - Test specific scenarios
5. **Add assertions** - Validate results automatically
6. **Use descriptive names** - Makes logs easier to read
7. **Follow email conventions** - `test-*/demo-*` for easy cleanup

## Validation Rules

- ✅ `version` must be "2.0"
- ✅ `metadata.name` is required
- ✅ `setup.user.email` is required and must be unique
- ✅ `setup.user.tier` is required
- ✅ `accounts[].item_index` must be valid (< linked_items count)
- ✅ `transactions[].account_index` must be valid (< accounts count)
- ✅ `assertions[].type` must be known type
- ✅ Balance values must be numeric
- ✅ Dates must be YYYY-MM-DD format

## Troubleshooting

**"User email is required"** - Add `setup.user.email`
**"Invalid item_index"** - Reference valid `linked_items` array index (0-based)
**"Invalid account_index"** - Reference valid `accounts` array index (0-based)
**"Database connection failed"** - Check `backend/.env` has `DATABASE_URL`
**"yq command not found"** - Install: `brew install yq`

## More Examples

See the example scenarios in this directory:

**For Backend/API Testing (mode: new):**
- `01-demo-data-free-user.yaml` - Basic free tier
- `02-demo-data-premium-user.yaml` - Premium with multiple accounts
- `03-ui-testing-multi-account.yaml` - Many accounts for pagination
- `04-tier-limit-testing.yaml` - Free tier at maximum (3 accounts)
- `05-edge-cases-pending-refunds.yaml` - Manual transactions, edge cases

**For Frontend Testing (mode: existing):**
- `99-existing-user-example.yaml` - Template for real Clerk users (frontend testing)
