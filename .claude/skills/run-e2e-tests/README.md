# E2E Testing Skill

Run end-to-end tests locally for BoostedBudget using Playwright.

## Quick Start

```bash
cd frontend
set -a && source .env.e2e && set +a && \
mv cert.pem cert.pem.bak 2>/dev/null; mv key.pem key.pem.bak 2>/dev/null; \
bash scripts/run-e2e-with-ports.sh 2>&1 | tee e2e-output.log; \
mv cert.pem.bak cert.pem 2>/dev/null; mv key.pem.bak key.pem 2>/dev/null
```

## Architecture

### Test Infrastructure

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Playwright │────▶│   Frontend   │────▶│   Backend    │
│   (Browser)  │     │   (Vite)     │     │   (Go)       │
└──────────────┘     └──────────────┘     └──────────────┘
                                                 │
                                                 ▼
                                          ┌──────────────┐
                                          │  PostgreSQL  │
                                          │  (Docker)    │
                                          └──────────────┘
```

### Port Allocation

The script dynamically assigns ports to avoid conflicts:

| Service | Port Range |
|---------|------------|
| Frontend | 30000-34900 |
| Backend | 35000-39900 |
| PostgreSQL | 40000-44900 |

### Test Projects

| Project | Purpose |
|---------|---------|
| `setup` | Global Clerk setup (obtains testing token) |
| `auth` | Authentication setup (logs in, saves session) |
| `chromium` | Authenticated tests (uses saved session) |
| `chromium-no-auth` | Unauthenticated tests |

## Test Data Seeding

When running with Docker, the script automatically seeds:

1. **Test User** - Created via Clerk test mode
2. **3 Accounts** - Checking, Savings, Credit Card
3. **12+ Transactions** - Various categories and amounts
4. **User Tags** - Sample custom tags
5. **Categories** - Default category assignments

### Data Flow

```
1. Clerk login → obtains user ID
2. Database seeding → inserts test data for user
3. Tests run → interact with seeded data
4. Cleanup → removes Docker container
```

## Local vs CI Testing

### Local Testing

```bash
# Full test with Docker
bash scripts/run-e2e-with-ports.sh

# Skip database (mock API responses)
E2E_SKIP_POSTGRES=true bash scripts/run-e2e-with-ports.sh

# Specific test file
bash scripts/run-e2e-with-ports.sh e2e/transactions.spec.ts

# Specific project
bash scripts/run-e2e-with-ports.sh --project=chromium-no-auth
```

### CI Testing

In GitHub Actions, tests run with:
- Pre-built backend binary
- PostgreSQL service container
- Headless Chromium
- Fixed ports

See `.github/workflows/test.yml` for CI configuration.

## Common Failures & Debugging

### WebServer Timeout

**Symptoms:** "WebServer timeout waiting for server to start"

**Causes:**
1. SSL certificates causing HTTPS mode
2. Backend binary missing
3. Docker not running

**Solutions:**
```bash
# Move SSL certs
mv cert.pem cert.pem.bak && mv key.pem key.pem.bak

# Check backend binary
ls ../backend/bin/server

# Check Docker
docker info
```

### Authentication Failures

**Symptoms:** "CLERK_SECRET_KEY required" or auth tests skipped

**Solutions:**
```bash
# Load environment
set -a && source .env.e2e && set +a

# Verify variables
echo $CLERK_SECRET_KEY
echo $TEST_CLERK_EMAIL
```

### Test Data Issues

**Symptoms:** Tests fail with "account not found" or missing data

**Causes:**
1. Database not seeded
2. Seeding failed silently
3. Wrong user ID

**Solutions:**
```bash
# Check seeding logs
cat e2e-output.log | grep -i seed

# Verify database
psql $DATABASE_URL -c "SELECT * FROM accounts LIMIT 5"
```

### Port Conflicts

**Symptoms:** "Address already in use"

**Solutions:**
```bash
# Find conflicting process
lsof -i :30000

# Kill if needed
kill -9 <PID>

# Or use different port range
FRONTEND_PORT=31000 bash scripts/run-e2e-with-ports.sh
```

## Environment Variables

### Required

| Variable | Description |
|----------|-------------|
| `VITE_CLERK_PUBLISHABLE_KEY` | Clerk frontend key |
| `CLERK_SECRET_KEY` | Clerk backend key |
| `TEST_CLERK_EMAIL` | E2E test user email |
| `TEST_CLERK_PASSWORD` | E2E test user password |
| `ENCRYPTION_KEY` | 32-char encryption key |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `E2E_SKIP_POSTGRES` | false | Skip database, use mocks |
| `DATABASE_URL` | Auto-generated | PostgreSQL connection |
| `FRONTEND_PORT` | Dynamic | Override frontend port |
| `BACKEND_PORT` | Dynamic | Override backend port |

## Writing New Tests

### Basic Test Structure

```typescript
// e2e/feature.spec.ts
import { test, expect } from '@playwright/test'

test.describe('Feature', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/feature')
  })

  test('displays feature list', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Features' })).toBeVisible()
    await expect(page.getByTestId('feature-list')).toBeVisible()
  })

  test('creates new feature', async ({ page }) => {
    await page.getByRole('button', { name: 'Create' }).click()
    await page.getByLabel('Name').fill('Test Feature')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Test Feature')).toBeVisible()
  })
})
```

### Authenticated Tests

Use `chromium` project for tests requiring login:

```typescript
// playwright.config.ts already handles session storage
// Tests in chromium project automatically have auth
```

### Unauthenticated Tests

Use `chromium-no-auth` project:

```typescript
// e2e/landing.spec.ts
import { test, expect } from '@playwright/test'

test.use({ storageState: { cookies: [], origins: [] } })

test('shows landing page', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Sign In' })).toBeVisible()
})
```

## Related

- `SKILL.md` - Quick reference for running tests
- `frontend/playwright.config.ts` - Playwright configuration
- `frontend/scripts/run-e2e-with-ports.sh` - Test runner script
- `.github/workflows/test.yml` - CI configuration
