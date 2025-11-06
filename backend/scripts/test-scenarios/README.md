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

- **Issue #308** - Admin API for Manual Webhook Firing
- **Issue #309** - Role-Based Admin Authentication (future)
- **Issue #310** - Bulk Webhook Firing (future)
- **Issue #242** - Monthly Sync Limits
- **Issue #256** - Transaction Sync Service
- **Issue #257** - Item Error Handler

## License

Proprietary - BoostedBudget

---

**Last Updated:** 2025-11-05
