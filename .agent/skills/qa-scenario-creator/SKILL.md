---
name: qa-scenario-creator
description: "Create new YAML scenario files for BoostedBudget qa data populator with guided questions"
---

# QA Scenario Creator

## Purpose

This skill helps you create new YAML scenario files for the BoostedBudget qa data populator framework. It guides you through an interactive workflow to define test data scenarios with accounts, transactions, and assertions.

## When to Use This Skill

Invoke this skill when you need to:
- Create a new QA test scenario
- Generate YAML configuration for database test data
- Add scenarios for UI testing, edge cases, or tier limit validation
- Populate QA environment with specific test data patterns

## Interactive Workflow

When the user requests to create a QA scenario, follow these steps systematically:

### Step 1: Gather Scenario Metadata

Ask the user for:
1. **Scenario name**: Short, descriptive name (e.g., "UI Testing - Multi-Account Pagination")
2. **Description**: Clear explanation of the scenario purpose (1-2 sentences)
3. **Category**: Choose from:
   - `demo` - Demo data for general use
   - `ui-testing` - Testing UI components (pagination, performance)
   - `edge-case` - Testing unusual scenarios (refunds, pending, errors)
   - `tier-limit` - Testing subscription tier limits
   - `integration` - Integration testing with external systems

4. **Tags**: Suggest tags based on category (e.g., demo → ["demo", "free-tier", "ui-testing"])

### Step 2: User Configuration

Ask about the user account:
1. **Email**: Default to "test-scenario-N@example.com" or ask for specific email
2. **Tier**: Choose from:
   - `free` - 3 accounts max, 30-day history
   - `premium` - 30 accounts max, 365-day history
   - `premium_plus` - Unlimited accounts, 730-day history

### Step 3: Linked Items (Banks)

Ask:
1. **Number of linked items** (default: 1 for simple scenarios, 2+ for multi-bank testing)
2. **Institution names** for each item (e.g., "Chase", "Wells Fargo", "Bank of America")

### Step 4: Accounts Configuration

For each account, gather:
1. **item_index**: Which linked item (0-indexed)
2. **type**: Choose from:
   - `checking` (maps to depository)
   - `savings` (maps to depository)
   - `credit_card` (maps to credit)
   - `loan` (maps to loan)
   - `investment` (maps to investment)
3. **name**: Account display name (e.g., "Chase Total Checking", "Emergency Fund")
4. **balance**: Current balance (positive for assets, negative for credit card debt)

**Tier-appropriate defaults:**
- Free tier: 3 accounts (checking, savings, credit_card)
- Premium: 5-10 accounts (varied types)
- Premium+: 10+ accounts (all types)

### Step 5: Transaction Generation

Ask which mode:

**Option A: Auto Mode** (randomized transactions)
- `mode: "auto"`
- `count_per_account`: Number of transactions per account
- Generates realistic random transactions (expenses ~90%, income ~10%, pending ~10%)
- Historical date range based on tier

**Option B: Manual Mode** (specific transactions)
- `mode: "manual"`
- Define each transaction with:
  - `account_index`: Which account (0-indexed)
  - `amount`: Transaction amount (negative for expenses, positive for income)
  - `date`: Transaction date (YYYY-MM-DD or relative like "today", "yesterday")
  - `name`: Merchant/description
  - `category`: Category string or array
  - `pending`: true/false
  - `user_notes`: Optional notes

### Step 6: Assertions

Ask what should be validated:
1. **Transaction count**: `transactions_count: ">= N"` or `"= N"`
2. **Account count**: `account_count: "= N"`
3. **User tier**: `user_tier: "= tier"`
4. **Balance range**: `total_balance: ">= N"` (optional)

Suggest reasonable assertions based on the configuration.

### Step 7: Generate and Save

1. **Auto-number the file**: Find next available number (01-05 exist, so start at 06)
2. **Generate filename**: `{number}-{category}-{slug}.yaml` (e.g., `06-ui-testing-pagination.yaml`)
3. **Create complete YAML** with proper structure and comments
4. **Save to**: `backend/scripts/qa-data-populator/scenarios/`
5. **Provide usage command**:
   ```bash
   cd backend/scripts/qa-data-populator
   OVERRIDE_USER_EMAIL="user@example.com" OVERRIDE_USER_MODE="existing" \
     ./populate.sh scenarios/{filename}
   ```

## YAML Structure Template

```yaml
version: "2.0"

metadata:
  name: "{scenario_name}"
  description: "{description}"
  tags: [{tags}]

setup:
  user:
    email: "{email}"
    tier: "{tier}"

  linked_items:
    - institution: "{bank_name}"

  accounts:
    - item_index: 0
      type: "{type}"
      name: "{name}"
      balance: {balance}

transactions:
  mode: "{auto|manual}"
  count_per_account: {N}  # For auto mode
  # OR
  list:  # For manual mode
    - account_index: 0
      amount: {amount}
      date: "{date}"
      name: "{name}"
      category: "{category}"
      pending: {true|false}

assertions:
  database:
    transactions_count: "{operator} {value}"
    account_count: "{operator} {value}"
    user_tier: "= {tier}"
```

## Validation Rules

Before generating the file, validate:
- ✅ Account types are valid: checking, savings, credit_card, loan, investment
- ✅ Tier is valid: free, premium, premium_plus
- ✅ Transaction mode is either "auto" with count_per_account OR "manual" with list
- ✅ Account item_index references exist (0-indexed to linked_items)
- ✅ Manual transaction account_index references exist (0-indexed to accounts)
- ✅ Assertions are reasonable for the configuration
- ✅ Free tier has ≤3 accounts, premium ≤30, premium+ unlimited

## Examples

**Simple Demo Scenario:**
```yaml
# 3 accounts, 15 transactions each (auto mode)
tier: free
accounts: 3 (checking, savings, credit_card)
transactions: auto, 15 per account
assertions: count >= 45, accounts = 3
```

**UI Testing Scenario:**
```yaml
# 10 accounts, 100 transactions each (auto mode)
tier: premium
accounts: 10 (varied types)
transactions: auto, 100 per account (1000 total)
assertions: count >= 1000, accounts = 10
```

**Edge Case Scenario:**
```yaml
# Specific pending transactions and refunds
tier: free
accounts: 3
transactions: manual (5 specific transactions with pending=true, refunds)
assertions: pending_count >= 2
```

## Pro Tips

1. **Use sensible defaults**: 1 linked item, 3 accounts for free tier, auto mode for large datasets
2. **Progressive disclosure**: Ask simple questions first (name, tier), then details (accounts, transactions)
3. **Provide examples**: Show sample values for each question
4. **Validate early**: Check tier limits before asking about accounts
5. **Auto-suggest**: Suggest account names, balances, transaction counts based on tier and category
6. **Comment the YAML**: Add helpful comments in generated files

## Troubleshooting

**Skill not invoking?**
- Use keywords: "staging scenario", "create scenario", "new staging data"
- Be specific: "I need a scenario for testing pagination"

**Invalid YAML generated?**
- Check account types match valid list
- Verify transaction mode has correct fields
- Ensure all required fields are present

**File conflicts?**
- Skill auto-numbers to avoid conflicts
- If manual numbering needed, check existing scenarios first

---

For detailed schema reference and examples, see REFERENCE.md
