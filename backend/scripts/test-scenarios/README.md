# BoostedBudget Test Scenarios Framework

**Configuration-driven E2E/integration test scripts for populating staging with comprehensive transaction data.**

## Overview

This framework uses YAML scenario files to define test cases that fire Plaid webhooks via the admin endpoint (`POST /api/v1/admin/plaid/fire-webhook`) and validate the complete transaction sync pipeline.

### Key Features

✅ **Configuration-Driven** - Add new scenarios by creating YAML files (no code changes)
✅ **Extensible** - Template system for reusable test data
✅ **Comprehensive** - Covers all transaction/budgeting scenarios
✅ **Replicatable** - Run same scenarios repeatedly for regression testing
✅ **Manual Testing** - Perfect for staging environment population

## Quick Start

### 1. Setup Environment

```bash
# Copy environment template
cp config/staging.env.example config/staging.env

# Edit with your staging credentials
vim config/staging.env
```

Required variables:
- `API_BASE_URL` - Staging API URL
- `AUTH_TOKEN` - Auth0 JWT token with admin access
- `DATABASE_URL` - Staging PostgreSQL connection string

### 2. Run a Scenario

```bash
# Source environment
source config/staging.env

# Run single scenario
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml

# Run all scenarios
./run-all-scenarios.sh
```

### 3. View Reports

```bash
# HTML report
open reports/latest.html

# JSON report
cat reports/latest.json
```

## Directory Structure

```
backend/scripts/test-scenarios/
├── README.md                      # This file
├── PROGRESS.md                    # Implementation progress tracker
├── run-scenario.sh                # Main scenario runner
├── run-all-scenarios.sh          # Batch runner
├── lib/                          # Shared libraries
│   ├── api-client.sh             # Admin API wrapper
│   ├── db-utils.sh               # PostgreSQL helpers
│   ├── assertions.sh             # Test assertions
│   ├── fixtures.sh               # Data generators
│   └── reporting.sh              # HTML/JSON reports
├── scenarios/                    # YAML scenario definitions
│   ├── schema.yaml               # Scenario schema
│   ├── 01-free-tier-first-sync.yaml
│   ├── 02-pagination-multi-page.yaml
│   └── ...
├── templates/                    # Reusable test data
│   ├── transactions.yaml
│   ├── users.yaml
│   └── accounts.yaml
├── config/                       # Environment configs
│   ├── staging.env.example
│   └── local.env.example
└── reports/                      # Generated reports (gitignored)
    └── *.{json,html}
```

## Scenario File Format

### Basic Structure

```yaml
version: "1.0"

scenario:
  name: "free-tier-first-sync"
  description: "Test first transaction sync for free tier user"
  category: "normal"  # normal|edge_case|error|tier_limit|special
  tags: ["tier:free", "sync:first"]

  setup:
    user:
      id: "test-user-free-001"
      tier: "free"
      email: "freeuser@test.com"

    linked_items:
      - id: "item-free-001"
        user_id: "test-user-free-001"
        item_id: "item-plaid-sandbox-001"
        institution_name: "Chase"
        cursor: null
        status: "active"

    accounts:
      - id: "acc-free-001"
        linked_item_id: "item-free-001"
        plaid_account_id: "plaid-acc-checking-001"
        name: "Chase Checking"
        type: "depository"
        subtype: "checking"
        current_balance: 1000.00

  steps:
    - action: "fire_webhook"
      webhook_code: "INITIAL_UPDATE"
      item_id: "item-plaid-sandbox-001"

    - action: "wait"
      wait_seconds: 5

  assertions:
    database:
      transactions_count: ">= 1"
      sync_audits_count: 1

    webhook_status:
      processed: true
      error: null
```

### Supported Actions

**`fire_webhook`** - Fire a Plaid webhook
```yaml
- action: "fire_webhook"
  webhook_code: "DEFAULT_UPDATE"  # INITIAL_UPDATE|DEFAULT_UPDATE|HISTORICAL_UPDATE|TRANSACTIONS_REMOVED
  item_id: "item-plaid-sandbox-001"
```

**`wait`** - Wait for async processing
```yaml
- action: "wait"
  wait_seconds: 5
```

**`query_db`** - Execute custom SQL query (advanced)
```yaml
- action: "query_db"
  query: "SELECT COUNT(*) FROM transactions WHERE user_id = 'test-user-001'"
```

## Adding New Scenarios

### Step 1: Create YAML File

```bash
# Copy template
cp scenarios/00-template.yaml scenarios/99-my-new-scenario.yaml

# Edit scenario
vim scenarios/99-my-new-scenario.yaml
```

### Step 2: Define Test Data

Use templates for common patterns:

```yaml
setup:
  user:
    <<: *free_user_template  # Reference template
    id: "my-custom-user-id"
```

### Step 3: Run Scenario

```bash
./run-scenario.sh scenarios/99-my-new-scenario.yaml
```

### Step 4: Add to Batch Runner (Optional)

```bash
echo './run-scenario.sh scenarios/99-my-new-scenario.yaml "$@"' >> run-all-scenarios.sh
```

## Available Templates

### Transaction Templates (`templates/transactions.yaml`)

- `standard_purchase` - Regular debit transaction
- `pending_purchase` - Pending transaction
- `refund` - Credit/refund transaction
- `paycheck` - Income transaction
- `foreign_currency` - Non-USD transaction

### User Templates (`templates/users.yaml`)

- `free_user` - Free tier (3 accounts, 30 days)
- `premium_user` - Premium tier (30 accounts, 730 days)
- `premium_plus_user` - Premium+ tier (unlimited)

### Account Templates (`templates/accounts.yaml`)

- `checking_account` - Standard checking account
- `savings_account` - Savings account
- `credit_card` - Credit card account

## Available Scenarios

The framework includes 18 comprehensive test scenarios organized by priority and category. All scenarios are production-ready and documented with business value, implementation notes, and edge cases.

### Quick Reference Table

| # | Scenario | Priority | Category | LOC | Key Test |
|---|----------|----------|----------|-----|----------|
| 01 | Free Tier First Sync | P0 | Normal | 145 | 30-day history limit |
| 02 | Pagination Multi-Page | P0 | Normal | 232 | >500 transactions |
| 03 | Preserve User Notes | P0 | Edge Case | 189 | **CRITICAL** data loss prevention |
| 04 | Tier Limit Enforcement | P0 | Tier Limit | 215 | Monthly sync limits |
| 05 | Item Error Handling | P0 | Error | 178 | LOGIN_REQUIRED errors |
| 06 | Pending to Posted | P0 | Normal | 487 | Transaction state transitions |
| 07 | Removed Transactions | P0 | Normal | 395 | TRANSACTIONS_REMOVED webhook |
| 08 | Foreign Exchange | DEFER | Special | - | FX rates (Plaid limitation) |
| 09 | Refunds and Credits | P1 | Normal | 301 | Positive amount transactions |
| 10 | Inter-Account Transfers | P1 | Edge Case | 438 | Net-zero validation |
| 11 | Historical Backfill | P1 | Normal | 267 | Premium tier 730-day depth |
| 12 | Monthly Spending Patterns | P1 | Normal | 1,066 | Realistic category distribution |
| 13 | Recurring Subscriptions | P1 | Normal | 539 | Subscription detection |
| 14 | Large Purchases | P2 | Normal | 509 | Fraud alert thresholds |
| 15 | Merchant Diversity | P2 | Normal | 654 | 20+ unique merchants |
| 16 | Category Override & Manual Edits | P3 | Edge Case | 687 | User category preservation |
| 17 | Daily Micro-Transactions | P2 | Normal | 571 | "Latte factor" awareness |
| 18 | Seasonal Spending | P2 | Normal | 595 | Holiday spending spikes |

**Total**: 17 active scenarios (1 deferred), ~7,500 lines of test configuration

---

### P0 Scenarios (Week 1 - Critical Path)

#### 01. Free Tier First Sync
**File**: `scenarios/01-free-tier-first-sync.yaml`
**Business Value**: Validates 30-day historical limit for free tier users (most common tier)
**Test Coverage**:
- INITIAL_UPDATE webhook (first sync)
- 30-day transaction history depth
- Free tier account limit (3 accounts)
- Sync audit tracking

**Status**: ✅ Implemented (Issue #314)

#### 02. Pagination Multi-Page
**File**: `scenarios/02-pagination-multi-page.yaml`
**Business Value**: Prevents data loss when users have >500 transactions
**Test Coverage**:
- Plaid cursor-based pagination
- Multiple page fetching
- 500+ transaction datasets
- Cursor state management

**Status**: ✅ Implemented (Issue #314)

#### 03. Preserve User Notes (CRITICAL)
**File**: `scenarios/03-preserve-user-notes.yaml`
**Business Value**: **Data loss prevention** - user-entered notes must NEVER be overwritten
**Test Coverage**:
- User notes field preservation during UPDATE
- Plaid data updates don't overwrite user data
- Business logic separation (Plaid vs. user data)

**Status**: ✅ Implemented (Issue #314)
**Criticality**: ⚠️ This test MUST always pass - data loss is unacceptable

#### 04. Tier Limit Enforcement
**File**: `scenarios/04-tier-limit-enforcement.yaml`
**Business Value**: Ensures subscription tier limits are enforced (prevents revenue loss)
**Test Coverage**:
- Free tier: 3 accounts, 1 sync/day, 30 days
- Monthly sync count tracking
- Upgrade prompts when limits exceeded

**Status**: ✅ Implemented (Issue #314)

#### 05. Item Error Handling (LOGIN_REQUIRED)
**File**: `scenarios/05-item-error-login-required.yaml`
**Business Value**: Gracefully handles Plaid item errors (notify user, don't crash)
**Test Coverage**:
- LOGIN_REQUIRED error handling
- Item status updates (active → error)
- User notification creation
- Retry logic

**Status**: ✅ Implemented (Issue #314)

#### 06. Pending to Posted Transitions
**File**: `scenarios/06-pending-to-posted.yaml` (487 lines)
**Business Value**: Accurately tracks transaction state changes (pending → posted amounts may differ)
**Test Coverage**:
- Pre-populated pending transactions
- Amount changes when posted (gas pre-auth $50 → actual $47.23)
- Date changes (authorized_date → settlement date)
- Pending flag flip (true → false)

**Key Assertion**:
```sql
-- Verify pending→posted transition
SELECT pending, amount, date
FROM transactions
WHERE external_transaction_id = 'gas-001';
-- Expected: pending=false, amount=-47.23, date='2024-01-17'
```

**Status**: ✅ Implemented (Issue #332, Week 1)

#### 07. Removed Transactions (TRANSACTIONS_REMOVED)
**File**: `scenarios/07-removed-transactions.yaml` (395 lines)
**Business Value**: Handles retroactive transaction deletions from Plaid (bank corrections)
**Test Coverage**:
- TRANSACTIONS_REMOVED webhook handling
- Hard delete logic (not soft delete)
- Specific transaction targeting via removed_transactions array
- Account balance recalculation

**Key Assertion**:
```sql
-- Verify hard delete (transaction should NOT exist)
SELECT COUNT(*) FROM transactions
WHERE external_transaction_id = 'duplicate-charge-001';
-- Expected: 0 (deleted)
```

**Status**: ✅ Implemented (Issue #332, Week 1)

---

### P1 Scenarios (Week 2 - Common Patterns)

#### 09. Refunds and Credits
**File**: `scenarios/09-refunds-and-credits.yaml` (301 lines)
**Business Value**: Accurately tracks positive-amount transactions (refunds, cashback, reimbursements)
**Test Coverage**:
- Full refunds (100% of original purchase)
- Partial refunds (<100% - price adjustments)
- Cashback rewards (no original purchase)
- Employer reimbursements

**Key Pattern**: All refunds/credits have `amount > 0` (positive)

**Example**:
```yaml
# Original purchase
- amount: -129.99
  name: "Amazon.com - Laptop Stand"

# Full refund
- amount: 129.99  # Positive!
  name: "Amazon.com - Refund"
```

**Status**: ✅ Implemented (Issue #332, Week 2)

#### 10. Inter-Account Transfers
**File**: `scenarios/10-inter-account-transfers.yaml` (438 lines)
**Business Value**: Net-zero validation - transfers shouldn't impact net worth or inflate spending
**Test Coverage**:
- Transfer pair creation (out + in = $0)
- Net worth unchanged validation
- 3-account scenario (checking, savings, credit card)
- Date matching between pairs

**CRITICAL Assertion**:
```sql
-- Transfers MUST net to zero
SELECT SUM(amount) as net_transfer_amount
FROM transactions
WHERE name ILIKE '%transfer%';
-- Expected: 0.00 (exactly zero)
```

**Status**: ✅ Implemented (Issue #332, Week 2)

#### 11. Historical Backfill (HISTORICAL_UPDATE)
**File**: `scenarios/11-historical-backfill.yaml` (267 lines)
**Business Value**: Tests premium tier 730-day historical data depth
**Test Coverage**:
- HISTORICAL_UPDATE webhook
- Premium tier 730-day limit
- Backfill for existing users
- Date range validation (2023-01-15 to 2024-11-15)

**Status**: ✅ Implemented (Issue #314, Week 1)

#### 12. Monthly Spending Patterns
**File**: `scenarios/12-monthly-spending-patterns.yaml` (1,066 lines - most comprehensive)
**Business Value**: Realistic 30-day spending distribution for budget analysis testing
**Test Coverage**:
- 60 transactions across 30 days
- Category distribution: Groceries 25%, Restaurants 20%, Gas 15%, Shopping 20%, etc.
- 25+ unique transaction dates (no clustering)
- 8+ spending categories
- Realistic amount ranges per category

**Distribution**:
- Groceries: 12 transactions ($50-$150 each)
- Restaurants: 10 transactions ($15-$80 each)
- Gas: 8 transactions ($30-$70 each)
- Subscriptions: 9 transactions (fixed amounts)
- Coffee: 6 transactions ($4-$8 each)
- Income: 2 paychecks ($2,000-$4,000 each)

**Status**: ✅ Implemented (Issue #332, Week 2)

#### 13. Recurring Subscriptions
**File**: `scenarios/13-recurring-subscriptions.yaml` (539 lines)
**Business Value**: Subscription detection for "subscription fatigue" feature (Phase 3)
**Test Coverage**:
- 3 subscriptions × 3 months = 9 transactions
- Same merchant each month (exact match)
- Same amount each month (within $0.01)
- ~30 day cadence (28-32 day tolerance)

**Subscriptions**:
- Netflix: $15.99/month
- Spotify: $10.99/month
- Planet Fitness: $10.00/month

**Key Assertion**:
```sql
-- Verify exact amount each month
SELECT MIN(amount), MAX(amount)
FROM transactions WHERE name = 'Netflix';
-- Expected: -15.99, -15.99 (no variation)
```

**Status**: ✅ Implemented (Issue #332, Week 2)

---

### P2 Scenarios (Week 3 - Advanced Features)

#### 14. Large Purchases (Fraud Detection)
**File**: `scenarios/14-large-purchases.yaml` (509 lines)
**Business Value**: Fraud alerts, savings goals, budget shock warnings for purchases >$500
**Test Coverage**:
- 8 large purchases (>$500): rent, tuition, flight, laptop, etc.
- 7 normal purchases for baseline
- Largest: $3,500 (tuition)
- Smallest large: $725 (furniture)
- Average large: ~$1,470

**Use Cases**:
- "Unusual large transaction detected: $1,899 laptop"
- "Create savings goal for next $3,500 tuition payment?"
- "Warning: $2,400 rent will exceed monthly budget"

**Status**: ✅ Implemented (Issue #332, Week 3)

#### 15. Merchant Diversity
**File**: `scenarios/15-merchant-diversity.yaml` (654 lines)
**Business Value**: Realistic spending across 20+ merchants (loyalty analysis, rewards optimization)
**Test Coverage**:
- 30 transactions across 22 unique merchants
- 4 merchants appear 2× (coffee shops, gas stations)
- 18 merchants appear 1×
- No merchant >20% of total spending

**Categories**:
- Groceries: 4 stores (Whole Foods, Safeway, Trader Joe's, Target)
- Restaurants: 6 places (Chipotle, Panera, Olive Garden, etc.)
- Coffee: 3 shops (Starbucks, Dunkin', Peet's)
- Gas: 3 stations (Shell, Chevron, BP)

**Status**: ✅ Implemented (Issue #332, Week 3)

#### 17. Daily Micro-Transactions ("Latte Factor")
**File**: `scenarios/17-daily-micro-transactions.yaml` (571 lines)
**Business Value**: Spending awareness - small daily purchases (<$5) accumulate to large totals
**Test Coverage**:
- 20 micro-transactions over 10 business days
- All amounts <$5.00
- Categories: Coffee (10×), Transit (6×), Snacks (4×)
- Monthly projection: $97 for coffee alone

**The "Latte Factor"**:
- $4.85 coffee × 20 workdays = $97/month
- $97/month × 12 months = $1,164/year!

**Key Assertion**:
```sql
-- Monthly coffee projection
SELECT SUM(ABS(amount)) / 10 * 20 as monthly_projection
FROM transactions
WHERE detailed = 'FOOD_AND_DRINK_COFFEE';
-- Expected: $90-$105/month
```

**Status**: ✅ Implemented (Issue #332, Week 3)

#### 18. Seasonal Spending (Holiday Patterns)
**File**: `scenarios/18-seasonal-spending.yaml` (595 lines)
**Business Value**: Holiday budget planning - Thanksgiving + Black Friday spending spikes
**Test Coverage**:
- 4 weeks of November spending
- Week 1-2: Normal ($400/week baseline)
- Week 3: Pre-holiday increase ($650 - 1.6× baseline)
- Week 4 (Thanksgiving): Spike ($900 - 2.25× baseline)
- Black Friday weekend: Surge ($850)

**Key Assertion**:
```sql
-- Week-over-week multiplier
SELECT (week4_spending / week1_spending) as multiplier;
-- Expected: ≥2.0× (Thanksgiving week double normal)
```

**Status**: ✅ Implemented (Issue #332, Week 3)

---

### P3 Scenarios (Week 4 - Edge Cases)

#### 16. Category Override & Manual Edits
**File**: `scenarios/16-category-override-manual-edits.yaml` (687 lines)
**Business Value**: "I know this better than AI" - preserve user manual category overrides
**Test Coverage**:
- 8 transactions: 3 with user overrides, 5 with Plaid defaults
- User overrides MUST be preserved across syncs
- Plaid categories still update when NOT overridden
- `user_category_override` boolean flag

**Example**:
```yaml
# Costco - User override: GENERAL_MERCHANDISE (Plaid thinks: FOOD_AND_DRINK)
- external_transaction_id: "costco-001"
  personal_finance_category:
    primary: "GENERAL_MERCHANDISE"
    detailed: "GENERAL_MERCHANDISE_ELECTRONICS"
  user_category_override: true  # Don't let Plaid overwrite this!
```

**Backend Logic Required**:
```go
if !existingTxn.UserCategoryOverride {
    txn.PersonalFinanceCategory = plaidTxn.PersonalFinanceCategory
} else {
    log.Info("Preserving user category override")
    // Keep existing category, don't update from Plaid
}
```

**Status**: ✅ Implemented (Issue #332, Week 4)

---

### Deferred Scenarios

#### 08. Foreign Exchange (FX) Transactions
**File**: `scenarios/08-foreign-exchange.yaml` (not created)
**Deferral Reason**: Plaid Sandbox doesn't support real-time FX rates or ISO currency codes
**Future Implementation**: Wait for Plaid Sandbox enhancement or test only in production

---

## Library Functions

### API Client (`lib/api-client.sh`)

```bash
# Fire webhook
fire_webhook "item-id" "DEFAULT_UPDATE"

# Wait for processing
wait_for_webhook_processed "item-id" 30  # 30s timeout

# Get webhook status
get_webhook_status "item-id"

# Test auth token
test_auth_token
```

### Database Utils (`lib/db-utils.sh`)

```bash
# Insert test data
insert_user "user-id" "free" "user@test.com"
insert_linked_item "item-id" "user-id" "plaid-item-id" "Chase"
insert_account "acc-id" "item-id" "user-id" "plaid-acc-id" "Checking" "depository" "checking" 1000.00

# Query data
get_transaction_count "user-id"
get_sync_audit_count "user-id" "2025-11"
get_user_notes "txn-id" "user-id"

# Cleanup
cleanup_test_user "user-id"
```

### Assertions (`lib/assertions.sh`)

```bash
# Basic assertions
assert_equal "$actual" "$expected" "Values should match"
assert_gt "$count" 0 "Count should be positive"

# Domain-specific assertions
assert_webhook_processed "item-id"
assert_transaction_count "user-id" 10
assert_sync_audit_count "user-id" 3
assert_user_notes_preserved "txn-id" "user-id" "My note"
assert_linked_item_status "item-id" "active"

# Print summary
print_assertion_summary  # Returns 0 if all passed, 1 if any failed
```

## Scenario Categories

### Normal Flow
- First sync (INITIAL_UPDATE)
- Incremental sync (DEFAULT_UPDATE)
- Historical backfill (HISTORICAL_UPDATE)
- Removed transactions (TRANSACTIONS_REMOVED)

### Edge Cases
- Multi-page pagination (>500 transactions)
- Preserve user notes on update (**CRITICAL**)
- Pending → Posted transitions
- Date range boundaries
- Account mapping failures

### Error Scenarios
- Plaid API errors (transient, permanent, rate limit)
- Item errors (LOGIN_REQUIRED, PENDING_EXPIRATION, USER_PERMISSION_REVOKED)

### Tier Limits
- Free tier monthly sync limits (3 accounts)
- Premium tier historical depth (730 days)
- Premium+ unlimited

### Special Cases
- Refunds and adjustments
- Foreign currency transactions
- Multi-account transfers

## Troubleshooting

### "Webhook firing failed with HTTP 401"
- Check `AUTH_TOKEN` is valid Auth0 JWT
- Run `test_auth_token` to verify

### "Webhook firing failed with HTTP 404"
- Endpoint only available in staging environment
- Verify `API_BASE_URL` points to staging

### "Timeout waiting for webhook processing"
- Check webhook processor is running (`ps aux | grep webhook`)
- Verify database connection
- Increase timeout: `wait_for_webhook_processed "item-id" 60`

### "Database connection failed"
- Verify `DATABASE_URL` is correct
- Test connection: `psql "$DATABASE_URL" -c "SELECT 1"`
- Check network access to database

### "User notes not preserved"
- **CRITICAL BUG** - This test must always pass
- Check `TransactionSyncService.processModifiedTransactions()`
- Verify `user_notes` field preserved during UPDATE

## Best Practices

1. **Always cleanup test data** - Set `CLEANUP_AFTER_TEST=true`
2. **Use meaningful test IDs** - `test-user-free-tier-001` not `test-123`
3. **Document edge cases** - Add comments in YAML for unusual scenarios
4. **Version scenarios** - Commit YAML files to Git
5. **Run locally first** - Test against local Docker before staging
6. **Monitor database size** - Cleanup old test data periodically

## Development

### Running Tests Locally

```bash
# Start local stack
docker-compose up -d

# Use local config
source config/local.env

# Run scenario
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml --env local
```

### Contributing New Scenarios

1. Create scenario YAML file
2. Test locally
3. Test on staging
4. Document in PROGRESS.md
5. Commit to Git
6. Open PR with scenario description

## References

- **Issue #308** - Admin API for Manual Webhook Firing (implemented)
- **Issue #314** - Test Scenarios Framework - Phase 1-2 (scenarios 01-05, 11, templates, infrastructure)
- **Issue #332** - Advanced Transaction Test Scenarios - Phase 3-4 (scenarios 06-07, 09-10, 12-18)
- **Issue #309** - Role-Based Admin Authentication (future)
- **Issue #310** - Bulk Webhook Firing (future)
- **Issue #242** - Monthly Sync Limits
- **Issue #256** - Transaction Sync Service
- **Issue #257** - Item Error Handler

## Related Documentation

- **HOW-IT-WORKS.md** - Technical architecture and implementation details
- **DATASETS.md** - Comprehensive guide to templates, fixtures, and dataset patterns
- **PROGRESS.md** - Implementation progress and knowledge base

## License

Proprietary - BoostedBudget

---

**Last Updated:** 2025-11-06 (Issue #332 - Advanced Scenarios Complete)
