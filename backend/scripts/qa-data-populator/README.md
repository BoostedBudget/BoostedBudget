# QA Data Populator

YAML-based script suite for populating qa environment with fake test data. Generates realistic users, accounts, and transactions for UI testing, QA validation, and demo purposes.

## Quick Start

### Basic Usage (No Webhooks)
```bash
cd backend/scripts/qa-data-populator

# Run a demo scenario
./populate.sh scenarios/01-demo-data-free-user.yaml
```

### With Webhook Integration (Balance Snapshots)
```bash
cd backend/scripts/qa-data-populator

# Export Auth0 token
export AUTH_TOKEN="your-jwt-token"

# Run scenario with webhook firing
./populate.sh scenarios/06-balance-snapshot-creation.yaml
```

**Output (Basic Scenario):**
```
🚀 QA Data Populator

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
✅ **Fast** - Populate QA in <10 seconds
✅ **Idempotent** - Safe to run multiple times (auto-cleanup)
✅ **Extensible** - YAML-based, easy to add new scenarios
✅ **AI-Friendly** - Optimized for Claude Code to generate YAMLs
✅ **Webhook Integration** - NEW: Fire Plaid webhooks to create balance snapshots (Issue #353)

## Use Cases

### 1. **QA Manual Testing**
Populate QA with test data for UI validation.
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

### 5. **Frontend Testing with Real Users** ⭐

**The scenarios above create FAKE Auth0 users that CANNOT be used in the frontend.**

For frontend testing (viewing data in the UI), use `mode: existing` with a real Auth0 user:

**Step 1: Log into staging to create your user**
```bash
# Visit staging and log in via Auth0
open https://staging.boostedbudget.com
# Use Google, Microsoft, or any auth provider
```

**Step 2: Find your email**
```bash
cd backend/scripts/qa-data-populator
./helpers/list-users.sh
```

Output:
```
      email                  | subscription_tier | auth0_user_id           | accounts | transactions
-----------------------------|-------------------|-------------------------|----------|-------------
your-email@example.com       | free              | google-oauth2|12345... | 0        | 0
```

**Step 3: Create or modify a scenario**
```yaml
# Copy scenarios/99-existing-user-example.yaml
# Change email to yours:
setup:
  user:
    mode: "existing"  # REQUIRED for real users!
    email: "your-email@example.com"  # Your Auth0 email
    tier: "premium"  # Optional tier upgrade
```

**Step 4: Run the populator**
```bash
./populate.sh scenarios/99-existing-user-example.yaml
```

**Step 5: View in frontend**
```bash
# Refresh the frontend - you should see the data!
open https://staging.boostedbudget.com
```

**Key Differences:**
| Feature | `mode: new` (default) | `mode: existing` |
|---------|----------------------|------------------|
| **Frontend Access** | ❌ Cannot log in | ✅ Can log in and view data |
| **User Creation** | Creates fake Auth0 user | Uses real Auth0 user |
| **Cleanup Behavior** | Deletes user completely | Preserves user, deletes only data |
| **Use Case** | Backend/API testing | Frontend/UI testing |

**Troubleshooting:**
- **"User not found"** → Log into staging.boostedbudget.com first
- **Frontend shows no data** → Verify `mode: existing` in YAML
- **Cannot log in** → Use the same Auth0 provider (Google/Microsoft/etc.)

**Helper Scripts:**
```bash
# List all users in database
./helpers/list-users.sh

# Filter by email pattern
./helpers/list-users.sh --email 'demo%'

# Show all users
./helpers/list-users.sh --all
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

### Environment Variables

**Required:**
- `DATABASE_URL` - PostgreSQL connection string (read from backend/.env)

**Optional (for webhook firing):**
- `AUTH_TOKEN` - Auth0 JWT token for admin webhook endpoint
  - Required only when using `webhooks.fire_after_setup: true` in scenarios
  - Enables balance snapshot creation via webhook processing
  - Obtain from staging Auth0 login (see "Webhook Integration" below)

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

### 6. Balance Snapshot Creation Test ⭐ NEW
**File:** `scenarios/06-balance-snapshot-creation.yaml`
**User:** `test-balance-snapshots@example.com`
**Tier:** Premium (3 accounts, 30 transactions, webhook-driven)
**Use:** Balance snapshot testing, webhook integration validation
**Requires:** AUTH_TOKEN environment variable

### 7. Dashboard Balance History Test ⭐ NEW
**File:** `scenarios/07-balance-history-dashboard-test.yaml`
**User:** `test-dashboard-ui@example.com`
**Tier:** Premium (4 accounts, 100 transactions, balance snapshots)
**Use:** Dashboard UI testing, balance history chart validation
**Requires:** AUTH_TOKEN environment variable

## Webhook Integration (Issue #353)

**NEW:** Fire Plaid webhooks after data insertion to create balance snapshots for dashboard testing.

### Setup
```bash
# Export your Auth0 JWT token (required for admin webhook endpoint)
export AUTH_TOKEN="your-auth0-jwt-token"

# Run a scenario with webhook firing enabled
./populate.sh scenarios/06-balance-snapshot-creation.yaml
```

### How It Works
1. Scenario inserts user, accounts, and transactions into PostgreSQL
2. Script fires admin webhook via `POST /api/v1/admin/plaid/fire-webhook`
3. Webhook processing creates balance snapshots in `balance_snapshots` table
4. Assertions validate snapshots were created correctly

### Webhook-Enabled Scenarios
```bash
# Balance snapshot creation test
export AUTH_TOKEN="..." && ./populate.sh scenarios/06-balance-snapshot-creation.yaml

# Dashboard balance history test (4 accounts)
export AUTH_TOKEN="..." && ./populate.sh scenarios/07-balance-history-dashboard-test.yaml
```

### Example Webhook Configuration in YAML
```yaml
webhooks:
  fire_after_setup: true        # Fire webhook after data insertion
  webhook_code: "DEFAULT_UPDATE" # Webhook type (DEFAULT_UPDATE, INITIAL_UPDATE, etc.)
  wait_for_processing: true     # Wait for async processing to complete
  timeout_seconds: 30           # Maximum wait time

assertions:
  - type: "balance_snapshot_count"
    operator: "="
    value: 3  # One snapshot per account for today
```

### Obtaining AUTH_TOKEN
```bash
# Option 1: Log into staging and extract token from browser DevTools
# 1. Open https://staging.boostedbudget.com
# 2. Open DevTools → Application → Local Storage
# 3. Copy the auth token value

# Option 2: Use Auth0 CLI (if configured)
auth0 test login
```

**Security Note:** Webhook endpoint is staging-only and requires valid Auth0 JWT authentication.

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

### Webhook Issues

**"AUTH_TOKEN not set. Skipping webhook firing."**
```bash
# Export your Auth0 JWT token before running
export AUTH_TOKEN="eyJhbGc..."
./populate.sh scenarios/06-balance-snapshot-creation.yaml
```

**"Webhook firing failed (401 Unauthorized)"**
- Token expired: Auth0 JWT tokens expire after 24 hours
- Invalid token: Ensure token is from staging Auth0 tenant
- Solution: Get a fresh token from staging.boostedbudget.com

**"Webhook firing failed (500 Internal Server Error)"**
- Check backend logs: `fly logs -a boostedbudget-backend-staging`
- Verify admin webhook endpoint is deployed to staging
- Check if item_id exists in database

**"Timeout waiting for webhook processing"**
```yaml
webhooks:
  timeout_seconds: 60  # Increase timeout for many accounts
```
- Default timeout is 30 seconds
- Increase for scenarios with >5 accounts
- Check backend logs for processing errors

**"Balance snapshots not created"**
- Verify webhook was fired successfully (check logs)
- Check webhook.processed = true in database:
  ```sql
  SELECT * FROM webhooks WHERE item_id = 'your-item-id' ORDER BY created_at DESC LIMIT 1;
  ```
- Verify balance_snapshots table exists:
  ```sql
  SELECT COUNT(*) FROM balance_snapshots WHERE user_id = 'your-user-id';
  ```

**"balance_snapshot_count assertion failed"**
- Snapshots may already exist from previous run
- Solution: Cleanup runs automatically, but check manually:
  ```sql
  DELETE FROM balance_snapshots WHERE user_id = (SELECT id FROM users WHERE email = 'test-email@example.com');
  ```

## Architecture

### File Structure
```
qa-data-populator/
├── populate.sh              # Main orchestrator
├── lib/
│   ├── config.sh            # Load backend/.env
│   ├── yaml-parser.sh       # Parse YAML scenarios
│   ├── logging.sh           # Structured logging
│   ├── db-operations.sh     # PostgreSQL helpers (includes balance snapshot operations)
│   ├── cleanup.sh           # Idempotent cleanup (includes balance snapshot cleanup)
│   ├── setup-user.sh        # User creation
│   ├── setup-items.sh       # Linked items creation
│   ├── setup-accounts.sh    # Accounts creation
│   ├── fake-data-generator.sh  # Transaction generation
│   ├── admin-webhook.sh     # Admin webhook firing (NEW - Issue #353)
│   └── assertions.sh        # Validation framework (includes balance_snapshot_count)
├── scenarios/
│   ├── README.md            # Schema documentation
│   ├── 01-demo-data-free-user.yaml
│   ├── 02-demo-data-premium-user.yaml
│   ├── 03-ui-testing-multi-account.yaml
│   ├── 04-tier-limit-testing.yaml
│   ├── 05-edge-cases-pending-refunds.yaml
│   ├── 06-balance-snapshot-creation.yaml      # NEW - Balance snapshot testing
│   └── 07-balance-history-dashboard-test.yaml # NEW - Dashboard UI testing
└── templates/               # (Future) Reusable YAML templates
```

### Execution Flow
1. Parse YAML scenario
2. Validate dependencies (yq, psql, jq)
3. Load DATABASE_URL from backend/.env
4. Cleanup existing data by email (idempotent, including balance snapshots)
5. Create user with specified tier
6. Create linked items (fake Plaid items)
7. Create accounts (fake Plaid account IDs)
8. Generate transactions (auto or manual)
9. **Fire webhooks (optional)** - NEW: Creates balance snapshots via admin endpoint
10. Run assertions (validate results, including balance_snapshot_count)
11. Display summary (includes balance snapshot count)

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

- **Fake Data Only** - Does not integrate with real Plaid API for data fetching
  - ✅ **NEW:** Admin webhook integration now available (Issue #353) for balance snapshot testing
  - Fires real webhooks to create balance snapshots via production code path
- **PostgreSQL Only** - Requires PostgreSQL database
- **macOS/Linux** - Shell scripts designed for Unix-like systems
- **Single Region** - Populates current environment only (no multi-region support)
- **Auth0 Token Required** - Webhook firing requires valid Auth0 JWT token

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
