# E2E Scenarios - YAML-Driven Regression Tests

This directory contains YAML-driven E2E test scenarios for BoostedBudget. These scenarios provide comprehensive regression testing for critical business logic, data integrity, and edge cases.

## Quick Start

### Local Development

```bash
# 1. Start test services (uses dynamic port assignment)
cd frontend/e2e
docker-compose -f docker-compose.test.yml up -d

# 2. Get dynamically assigned ports
POSTGRES_PORT=$(docker-compose -f docker-compose.test.yml port postgres-test 5432 | cut -d: -f2)
BACKEND_PORT=$(docker-compose -f docker-compose.test.yml port backend-test 8080 | cut -d: -f2)

echo "PostgreSQL: localhost:$POSTGRES_PORT"
echo "Backend: localhost:$BACKEND_PORT"

# 3. Export environment variables for tests
export TEST_DATABASE_URL="postgresql://testuser:testpass@localhost:$POSTGRES_PORT/boostedbudget_test"
export API_BASE_URL="http://localhost:$BACKEND_PORT"

# 4. Wait for services to be ready (30 seconds)
sleep 30

# 5. Run scenarios
cd ../
npm run test:e2e:scenarios
```

**Note**: Services use dynamic port assignment to avoid conflicts with dev environment. Ports are automatically discovered in CI/CD.

### CI/CD

Scenarios run automatically on pull requests via `.github/workflows/e2e-scenarios.yml`:
- ✅ Triggered on PRs to `main` and `staging`
- ✅ Parallel execution with 4 workers
- ✅ Worker-specific isolated databases
- ✅ Automatic artifact uploads on failure

## Scenario Structure

Each scenario consists of two files:

1. **YAML Definition** (`*.yaml`): Declarative test configuration
   - Setup: Users, accounts, transactions, linked items
   - Steps: API calls, webhooks, delays
   - Assertions: Database queries, API responses, UI checks
   - Cleanup: Database cleanup strategy

2. **Playwright Spec** (`*.spec.ts`): Test execution
   - Loads YAML via ScenarioLoader
   - Executes via ScenarioRunner
   - Additional specialized test methods
   - Detailed logging and assertions

## Current Scenarios

### P0 - Critical Business Logic (MUST PASS)

1. **`01-free-tier-first-sync.yaml`** - Free Tier History Limit
   - **Purpose**: Verify free tier users get exactly 30 days of transaction history
   - **Business Impact**: Revenue protection - prevents free users from accessing premium features
   - **Assertions**: 5 (4 database + 1 API)
   - **Key Test**: Generates 60 days of transactions, verifies only last 30 are synced

2. **`03-preserve-user-notes.yaml`** - User Notes Preservation
   - **Purpose**: Verify user notes on transactions are preserved during sync operations
   - **Business Impact**: Prevents critical data loss bug - users lose important notes
   - **Assertions**: 7 (6 database + 1 API)
   - **Key Test**: Creates transactions with user notes, fires DEFAULT_UPDATE webhook, verifies notes preserved

3. **`04-tier-limit-enforcement.yaml`** - Account Creation Limits
   - **Purpose**: Verify free tier 3-account monthly limit is enforced
   - **Business Impact**: Revenue protection - prevents free users from creating unlimited accounts
   - **Assertions**: 6 (4 database + 2 API)
   - **Key Test**: Creates 3 accounts (at limit), attempts 4th, verifies rejection with 403

### P1 - High Priority (Common Issues)

4. **`02-pagination-multi-page.yaml`** - Multi-Page Pagination
   - **Purpose**: Verify transaction sync handles >500 transactions across multiple pages
   - **Business Impact**: Ensures large transaction volumes are handled correctly
   - **Assertions**: 7 (6 database + 1 API)
   - **Key Test**: Generates 600 transactions, verifies all synced with no duplicates
   - **Common Bug**: Duplicate transactions across pages due to cursor handling issues

5. **`05-item-error-login-required.yaml`** - Error State Handling
   - **Purpose**: Verify graceful handling of Plaid item LOGIN_REQUIRED error state
   - **Business Impact**: Prevents data loss when credentials expire
   - **Assertions**: 8 (6 database + 2 API)
   - **Key Test**: Fires ITEM_LOGIN_REQUIRED webhook, verifies error state without data loss

### P2 - Medium Priority (Edge Cases)

6. **`07-removed-transactions.yaml`** - Retroactive Transaction Deletion
   - **Purpose**: Verify TRANSACTIONS_REMOVED webhook correctly deletes retroactively removed transactions
   - **Business Impact**: Maintains data consistency when financial institutions remove erroneous charges
   - **Assertions**: 8 (7 database + 1 API)
   - **Key Test**: Creates 5 transactions, removes 3 via webhook, verifies exact deletions
   - **Use Cases**: Duplicate charges, erroneous charges, reversed payments

7. **`06-pending-to-posted.yaml`** - State Transitions
   - **Purpose**: Verify pending transactions correctly transition to posted status
   - **Business Impact**: Accurate transaction tracking through authorization → settlement lifecycle
   - **Assertions**: 10 (8 database + 2 API)
   - **Key Test**: Creates 3 pending transactions, fires update with posted versions, verifies transition
   - **Use Cases**: Restaurant tips added, hotel checkout dates, gas pre-authorization → final charge

## Adding New Scenarios

### 1. Create YAML File

```yaml
# scenarios/transaction-sync/08-your-scenario.yaml
version: "1.0"

scenario:
  name: "your-scenario"
  description: "What this scenario tests"
  category: "transaction-sync"

  setup:
    user:
      id: "test-your-scenario-001"
      email: "your-scenario@test.example.com"
      tier: "free"
      auth0_user_id: "auto-generate"

    # ... accounts, transactions, etc.

  steps:
    - action: "api_call"
      method: "POST"
      endpoint: "/api/v1/plaid/webhook"
      body:
        webhook_type: "TRANSACTIONS"
        webhook_code: "DEFAULT_UPDATE"
      expect_status: 200

  assertions:
    database:
      - description: "Your assertion description"
        query: |
          SELECT COUNT(*) as count FROM transactions
        expected:
          count: 10

  cleanup:
    strategy: "truncate_all"
```

### 2. Create Playwright Spec

```typescript
// scenarios/transaction-sync/08-your-scenario.spec.ts
import { test, expect } from '@playwright/test';
import { Pool } from 'pg';
import { ScenarioRunner } from '../../lib/scenario-runner';
import { ScenarioLoader } from '../../lib/scenario-loader';

test.describe('Your Scenario', () => {
  let dbPool: Pool;
  let runner: ScenarioRunner;
  let loader: ScenarioLoader;

  test.beforeAll(async () => {
    // Setup...
  });

  test('should do what the scenario tests', async ({ page }) => {
    const scenario = await loader.load('08-your-scenario.yaml');
    const result = await runner.run(scenario, page);
    expect(result.success).toBe(true);
  });
});
```

### 3. Run Locally

```bash
cd frontend
npx playwright test e2e/scenarios/transaction-sync/08-your-scenario.spec.ts
```

## Templates

Reusable YAML templates in `templates/` directory:

- **`users.yaml`**: User tier templates (free, premium, trial, cancelled)
- **`accounts.yaml`**: Account types (checking, savings, credit card, loan, investment)
- **`transactions.yaml`**: Transaction patterns (purchases, income, refunds, recurring)
- **`budgets.yaml`**: Budget templates (future-ready)
- **`rules.yaml`**: Automation rule templates (future-ready)

### Using Templates

```yaml
setup:
  user:
    <<: *free_user  # Reference template from templates/users.yaml
    email: "override@example.com"  # Override specific fields
```

## Variable Interpolation

Reference setup values in steps and assertions:

```yaml
steps:
  - action: "api_call"
    endpoint: "/api/v1/transactions?account_id={{ accounts[0].id }}"

assertions:
  database:
    - query: |
        SELECT * FROM users WHERE email = '{{ user.email }}'
```

Supported syntax:
- `{{ user.id }}` - Simple property access
- `{{ accounts[0].name }}` - Array index access
- `{{ linked_items[0].item_id }}` - Nested access

## Architecture

### Core Modules (`lib/`)

- **`types.ts`**: Zod schemas and TypeScript types
- **`fixtures.ts`**: Faker.js generators for test data
- **`seeding.ts`**: Database seeding with batch operations
- **`scenario-loader.ts`**: YAML parser with validation
- **`api-client.ts`**: HTTP client wrapper
- **`assertions.ts`**: Database/API/UI assertion runners
- **`cleanup.ts`**: Cleanup manager with TRUNCATE CASCADE
- **`scenario-runner.ts`**: Main orchestrator (setup → steps → assertions → cleanup)

### Parallel Execution

4 worker databases for true test isolation:
- `boostedbudget_test_worker_0`
- `boostedbudget_test_worker_1`
- `boostedbudget_test_worker_2`
- `boostedbudget_test_worker_3`

Each worker:
- Gets its own isolated database
- Runs scenarios in parallel
- No test pollution
- 4x faster execution

## Troubleshooting

### Services Not Starting

```bash
# Check service status
docker-compose -f docker-compose.test.yml ps

# View logs
docker-compose -f docker-compose.test.yml logs postgres-test
docker-compose -f docker-compose.test.yml logs backend-test

# Restart services
docker-compose -f docker-compose.test.yml down -v
docker-compose -f docker-compose.test.yml up -d
```

### Database Connection Issues

```bash
# Verify PostgreSQL is ready
docker-compose -f docker-compose.test.yml exec postgres-test pg_isready -U test

# Check connections
docker-compose -f docker-compose.test.yml exec postgres-test psql -U test -c "SELECT count(*) FROM pg_stat_activity;"
```

### Worker Database Issues

```bash
# Recreate worker databases
cd frontend/e2e
export TEST_DATABASE_URL="postgresql://test:test@localhost:5433/boostedbudget_test"
npx tsx setup/create-test-databases.ts
```

## Performance

- **Batch inserts**: 10x faster than individual queries
- **Connection pooling**: Reuse connections across tests
- **TRUNCATE CASCADE**: 10x faster cleanup than DELETE
- **Parallel execution**: 4x faster with worker databases

## Documentation

- **`PROGRESS.md`**: Complete implementation log with decisions and patterns
- **`schema.yaml`**: Comprehensive schema reference with examples
- **`README.md`** (this file): Usage guide

## Future Scenarios

Easy to add following established patterns:

- Historical update webhook (backfilling old transactions)
- User journey scenarios (onboarding, upgrade, deletion)
- Budget scenarios (when budgets feature implemented)
- Rule scenarios (when rules feature implemented)
- Rate limiting / throttling scenarios
- Concurrent sync scenarios
- Multi-account sync scenarios
