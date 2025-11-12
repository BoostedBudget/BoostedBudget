# Staging Data Populator

YAML-based script suite for populating staging environment with fake test data. Generates realistic users, accounts, and transactions for UI testing, QA validation, and demo purposes.

## Quick Start

```bash
cd backend/scripts/staging-data-populator

# Run a demo scenario
./populate.sh scenarios/01-demo-data-free-user.yaml
```

**Output:**
```
🚀 Staging Data Populator

Scenario: Demo Data - Free Tier User
Tags: demo, free-tier, ui-testing

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  SETUP PHASE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🗑️  Cleaning up existing data for: demo-free@example.com
✅ User created: demo-free@example.com (tier: free, 30-day history)
[1/1] Linked item: Chase
[1/3] Account: Chase Total Checking (checking)
[2/3] Account: Chase Savings (savings)
[3/3] Account: Chase Freedom (credit_card)
✅ Created 3 account(s)
✅ Generated 45 transaction(s)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ASSERTIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✅ Transaction count: 47 >= 45
✅ Account count: 3 = 3
✅ User tier: free = free
✅ Assertions: 3 passed, 0 failed

✅ Scenario complete! (5.2s)

Results:
  User: demo-free@example.com
  Tier: free
  Accounts: 3
  Transactions: 47
  Historical days: 30
```

## Features

✅ **Realistic Fake Data** - 50+ merchants, proper categories, randomized amounts
✅ **Tier-Aware** - Free (30 days), Premium (365 days), Premium+ (730 days)
✅ **Fast** - Populate staging in <10 seconds
✅ **Idempotent** - Safe to run multiple times (auto-cleanup)
✅ **Extensible** - YAML-based, easy to add new scenarios
✅ **AI-Friendly** - Optimized for Claude Code to generate YAMLs

## Use Cases

### 1. **QA Manual Testing**
Populate staging with test data for UI validation.
```bash
./populate.sh scenarios/01-demo-data-free-user.yaml
./populate.sh scenarios/02-demo-data-premium-user.yaml
```

### 2. **Demo Data / Screenshots**
Create realistic data for product demos and marketing materials.
```bash
./populate.sh scenarios/02-demo-data-premium-user.yaml  # Premium tier with $100K+ balances
```

### 3. **Performance Testing**
Test pagination and performance with many accounts/transactions.
```bash
./populate.sh scenarios/03-ui-testing-multi-account.yaml  # 10 accounts, 1000+ transactions
```

### 4. **Edge Case Testing**
Test specific scenarios (pending transactions, refunds, user notes).
```bash
./populate.sh scenarios/05-edge-cases-pending-refunds.yaml
```

## Prerequisites

### Required Dependencies

- **yq** - YAML parser
  ```bash
  brew install yq
  ```

- **psql** - PostgreSQL client
  ```bash
  brew install postgresql
  ```

- **jq** - JSON parser
  ```bash
  brew install jq
  ```

### Configuration

The script reads `DATABASE_URL` from `backend/.env` (no additional configuration needed).

**Verify backend/.env exists:**
```bash
ls ../../.env
```

**Example backend/.env:**
```env
DATABASE_URL=postgresql://user:pass@localhost:5432/boostedbudget
```

## Available Scenarios

### 1. Demo Data - Free Tier User
**File:** `scenarios/01-demo-data-free-user.yaml`
**User:** `demo-free@example.com`
**Tier:** Free (3 accounts, 45 transactions, 30-day history)
**Use:** QA testing, basic UI validation

### 2. Demo Data - Premium User
**File:** `scenarios/02-demo-data-premium-user.yaml`
**User:** `demo-premium@example.com`
**Tier:** Premium (5 accounts, 250+ transactions, 2-year history)
**Use:** Demos, screenshots, premium feature testing

### 3. UI Testing - Multi-Account Pagination
**File:** `scenarios/03-ui-testing-multi-account.yaml`
**User:** `test-multi-account@example.com`
**Tier:** Premium (10 accounts, 1000+ transactions)
**Use:** Performance testing, pagination testing

### 4. Tier Limit Testing
**File:** `scenarios/04-tier-limit-testing.yaml`
**User:** `test-tier-limit-free@example.com`
**Tier:** Free (exactly 3 accounts - maximum for free tier)
**Use:** Tier limit validation

### 5. Edge Cases - Pending & Refunds
**File:** `scenarios/05-edge-cases-pending-refunds.yaml`
**User:** `test-edge-cases@example.com`
**Tier:** Premium Lite (manual transactions with pending, refunds, notes)
**Use:** Edge case testing

## Creating New Scenarios

### 1. Copy Existing Scenario
```bash
cp scenarios/01-demo-data-free-user.yaml scenarios/my-custom-scenario.yaml
```

### 2. Edit YAML
```yaml
version: "2.0"
metadata:
  name: "My Custom Scenario"
  tags: ["custom", "testing"]

setup:
  user:
    email: "my-test-user@example.com"  # Must be unique
    tier: "premium"

  linked_items:
    - institution: "Chase"

  accounts:
    - item_index: 0
      type: "checking"
      name: "My Checking"
      balance: 5000.00

  transactions:
    count_per_account: 30  # Auto-generate 30 transactions

assertions:
  - type: "transaction_count"
    operator: ">="
    value: 30
```

### 3. Run Your Scenario
```bash
./populate.sh scenarios/my-custom-scenario.yaml
```

**For complete schema documentation, see `scenarios/README.md`**

## Command-Line Options

### Basic Usage
```bash
./populate.sh <scenario.yaml>
```

### Options
- `--no-cleanup` - Skip cleanup of existing data (for debugging)
- `--debug` - Enable debug logging
- `-h, --help` - Show help message

### Examples
```bash
# Run with debug logging
./populate.sh scenarios/01-demo-data-free-user.yaml --debug

# Run without cleanup (keep existing data)
./populate.sh scenarios/01-demo-data-free-user.yaml --no-cleanup

# Show help
./populate.sh --help
```

## Cleanup

### Cleanup Specific User
Scenarios automatically cleanup before running (idempotent).

### Manual Cleanup (All Test Data)
```bash
# WARNING: Deletes all users with test-* or demo-* emails
psql $DATABASE_URL <<EOF
DELETE FROM transactions WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'test-%' OR email LIKE 'demo-%');
DELETE FROM accounts WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'test-%' OR email LIKE 'demo-%');
DELETE FROM linked_items WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'test-%' OR email LIKE 'demo-%');
DELETE FROM users WHERE email LIKE 'test-%' OR email LIKE 'demo-%';
EOF
```

## Generated Data Characteristics

### Transactions (Auto-Generated)

**Merchants (50+ options):**
- Coffee: Starbucks, Peet's Coffee, Blue Bottle
- Retail: Amazon, Target, Walmart, Costco, Best Buy
- Groceries: Whole Foods, Trader Joe's, Safeway, Kroger
- Restaurants: McDonald's, Chipotle, Subway, Domino's
- Services: Netflix, Spotify, Uber, Lyft
- Utilities: AT&T, Verizon, Comcast
- And many more...

**Categories:**
- Food and Drink, Shopping, Transportation, Entertainment
- Gas, Groceries, Healthcare, Utilities, Home, Travel

**Amounts:**
- Expenses: -$5.00 to -$200.00 (random with cents)
- Income: $2,000 to $5,000 (10% of transactions)

**Dates:**
- Distributed across historical period (30/365/730 days based on tier)
- Random within tier's limit

**Pending Status:**
- 10% pending, 90% posted

## Troubleshooting

### "yq command not found"
```bash
brew install yq
```

### "psql command not found"
```bash
brew install postgresql
```

### "Database connection failed"
Check `backend/.env` has `DATABASE_URL`:
```bash
cat ../../.env | grep DATABASE_URL
```

### "Scenario file not found"
Use relative path from script directory:
```bash
./populate.sh scenarios/01-demo-data-free-user.yaml  # Correct
./populate.sh 01-demo-data-free-user.yaml  # Wrong
```

### "User email is required"
Add `setup.user.email` to your YAML:
```yaml
setup:
  user:
    email: "unique-email@example.com"
    tier: "free"
```

### "Invalid item_index"
`item_index` must reference a valid `linked_items` array index (0-based):
```yaml
linked_items:
  - institution: "Chase"  # index 0

accounts:
  - item_index: 0  # References linked_items[0] ✓
  - item_index: 1  # ERROR: linked_items[1] doesn't exist ✗
```

### Assertions Failing
Check expected vs. actual counts:
```yaml
assertions:
  - type: "transaction_count"
    operator: ">="
    value: 50  # If you have count_per_account: 10 across 3 accounts, this expects 50 but you'll only get 30
```

## Architecture

### File Structure
```
staging-data-populator/
├── populate.sh              # Main orchestrator
├── lib/
│   ├── config.sh            # Load backend/.env
│   ├── yaml-parser.sh       # Parse YAML scenarios
│   ├── logging.sh           # Structured logging
│   ├── db-operations.sh     # PostgreSQL helpers
│   ├── cleanup.sh           # Idempotent cleanup
│   ├── setup-user.sh        # User creation
│   ├── setup-items.sh       # Linked items creation
│   ├── setup-accounts.sh    # Accounts creation
│   ├── fake-data-generator.sh  # Transaction generation
│   └── assertions.sh        # Validation framework
├── scenarios/
│   ├── README.md            # Schema documentation
│   ├── 01-demo-data-free-user.yaml
│   ├── 02-demo-data-premium-user.yaml
│   ├── 03-ui-testing-multi-account.yaml
│   ├── 04-tier-limit-testing.yaml
│   └── 05-edge-cases-pending-refunds.yaml
└── templates/               # (Future) Reusable YAML templates
```

### Execution Flow
1. Parse YAML scenario
2. Validate dependencies (yq, psql, jq)
3. Load DATABASE_URL from backend/.env
4. Cleanup existing data by email (idempotent)
5. Create user with specified tier
6. Create linked items (fake Plaid items)
7. Create accounts (fake Plaid account IDs)
8. Generate transactions (auto or manual)
9. Run assertions (validate results)
10. Display summary

## Integration with Claude Code

This framework is optimized for Claude Code to generate scenarios on demand.

**Example User Request:**
> "Create a YAML for a premium user with 7 accounts and 200 transactions"

**Claude Code Generates:**
```yaml
version: "2.0"
metadata:
  name: "Custom Premium - 7 Accounts"
  tags: ["custom", "premium"]

setup:
  user:
    email: "custom-premium-7accts@example.com"
    tier: "premium"

  linked_items:
    - institution: "Chase"
    - institution: "Bank of America"

  accounts:
    - {item_index: 0, type: "checking", name: "Chase Checking", balance: 5000}
    - {item_index: 0, type: "savings", name: "Chase Savings", balance: 15000}
    - {item_index: 0, type: "credit_card", name: "Chase Card", balance: -2000}
    - {item_index: 1, type: "checking", name: "BofA Checking", balance: 3000}
    - {item_index: 1, type: "savings", name: "BofA Savings", balance: 10000}
    - {item_index: 1, type: "investment", name: "BofA Investment", balance: 50000}
    - {item_index: 1, type: "credit_card", name: "BofA Card", balance: -1500}

  transactions:
    count_per_account: 29  # 29×7 ≈ 200 transactions

assertions:
  - {type: "account_count", operator: "=", value: 7}
  - {type: "transaction_count", operator: ">=", value: 200}
```

**User Runs:**
```bash
./populate.sh scenarios/custom-premium-7accts.yaml
```

## Limitations

- **Fake Data Only** - Does not integrate with real Plaid API (no actual webhook testing)
- **PostgreSQL Only** - Requires PostgreSQL database
- **macOS/Linux** - Shell scripts designed for Unix-like systems
- **Single Region** - Populates current environment only (no multi-region support)

## Comparison to Deleted Framework

| Feature | Old Framework | New Framework |
|---------|---------------|---------------|
| **Lines of Code** | 8,000+ lines | ~1,800 lines |
| **Complexity** | 7 scenarios, 46 files | 5 scenarios, 19 files |
| **UUID Issues** | Yes (type validation errors) | No (runtime generation) |
| **Plaid Integration** | Real + Fake modes | Fake only (simplified) |
| **Template System** | Yes (complex) | No (copy-paste patterns) |
| **Maintenance** | High | Low |
| **Speed** | 30-60 sec (real mode) | <10 sec (fake mode) |

## Future Enhancements

- [ ] Template system for reusable patterns
- [ ] Bulk scenario execution (run all scenarios)
- [ ] JSON output format for CI/CD integration
- [ ] Database seed/restore functionality
- [ ] Support for additional assertion types
- [ ] Interactive mode (prompt for inputs)

## References

- **YAML Schema Guide:** `scenarios/README.md`
- **Example Scenarios:** `scenarios/*.yaml`
- **Backend Database Schema:** `backend/migrations/`
- **Subscription Tiers:** `config/subscription_tiers.yaml`

## Support

For issues or questions:
1. Check `scenarios/README.md` for schema documentation
2. Review example scenarios in `scenarios/`
3. Enable debug logging: `./populate.sh --debug scenarios/your-scenario.yaml`
4. Check troubleshooting section above

## License

Part of BoostedBudget project - see root LICENSE file
