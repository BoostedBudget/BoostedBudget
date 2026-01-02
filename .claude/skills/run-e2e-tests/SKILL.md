---
name: run-e2e-tests
description: "Run E2E tests locally for BoostedBudget. Use when testing frontend functionality, running playwright tests, or validating before PRs."
version: "1.0.0"
---

# BoostedBudget E2E Testing

## Quick Start

Run all E2E tests:
```bash
cd frontend
set -a && source .env.e2e && set +a
mv cert.pem cert.pem.bak 2>/dev/null; mv key.pem key.pem.bak 2>/dev/null
bash scripts/run-e2e-with-ports.sh
mv cert.pem.bak cert.pem 2>/dev/null; mv key.pem.bak key.pem 2>/dev/null
```

## Prerequisites

1. **Docker** running (for PostgreSQL container)
2. **Frontend dependencies** installed (`pnpm install`)
3. **Backend binary** exists (`../backend/bin/server`) - auto-built if missing
4. **Clerk credentials** in `frontend/.env.e2e`:
   - VITE_CLERK_PUBLISHABLE_KEY
   - CLERK_PUBLISHABLE_KEY
   - CLERK_SECRET_KEY
   - TEST_CLERK_EMAIL (for authenticated tests)
   - TEST_CLERK_PASSWORD
   - ENCRYPTION_KEY

## SSL Certificate Handling

Local SSL certificates (`cert.pem`, `key.pem`) cause Vite to serve HTTPS, but Playwright expects HTTP.
**Temporarily move certs before running tests:**
```bash
mv cert.pem cert.pem.bak && mv key.pem key.pem.bak
# Run tests...
mv cert.pem.bak cert.pem && mv key.pem.bak key.pem
```

## Common Commands

**Run all tests:**
```bash
bash scripts/run-e2e-with-ports.sh
```

**Run specific test file:**
```bash
bash scripts/run-e2e-with-ports.sh e2e/auth.spec.ts
```

**Run without database (if Docker unavailable):**
```bash
E2E_SKIP_POSTGRES=true bash scripts/run-e2e-with-ports.sh
```

**Run specific project:**
```bash
bash scripts/run-e2e-with-ports.sh --project=chromium
bash scripts/run-e2e-with-ports.sh --project=chromium-no-auth
```

## What the Script Does

1. Finds available ports (frontend 30000-34900, backend 35000-39900, postgres 40000-44900)
2. Starts PostgreSQL container (if Docker available)
3. Runs Atlas migrations
4. Seeds test data (accounts, transactions, tags) for E2E user
5. Builds backend binary (if missing)
6. Starts frontend (Vite) and backend servers
7. Runs Playwright tests
8. Cleans up PostgreSQL container on exit

## Test Data Seeding

When running with Docker, the script automatically seeds test data:
- 3 test accounts (checking, savings, credit card)
- 12+ transactions with various categories
- User tags and custom categories

The seeding uses the Clerk test user ID (obtained via API or `E2E_CLERK_USER_ID` env var).

## Test Projects

| Project | Description |
|---------|-------------|
| `chromium` | Authenticated tests (uses saved session from auth.setup.ts) |
| `chromium-no-auth` | Unauthenticated tests (no session) |
| `setup` | Global Clerk setup (obtains testing token) |
| `auth` | Authentication setup (logs in, saves session) |

## Clerk Test Mode

Use Clerk's built-in test mode for authentication:

| Feature | Value |
|---------|-------|
| **Test email format** | `any_email+clerk_test@example.com` |
| **Verification code** | `424242` |
| **Test phone numbers** | `+1555555XXXX` (100-199) |

Test user credentials are stored in `.env.e2e`:
- `TEST_CLERK_EMAIL=e2e+clerk_test@boostedbudget.com`
- `TEST_CLERK_PASSWORD=<password>`

## Troubleshooting

**"WebServer timeout"**
- Check if cert.pem/key.pem exist (need to move them temporarily)
- Check if backend binary exists: `ls ../backend/bin/server`
- Check Docker is running: `docker info`

**"CLERK_SECRET_KEY required"**
- Export env vars: `set -a && source .env.e2e && set +a`

**"ENCRYPTION_KEY required"**
- Add to .env.e2e: `ENCRYPTION_KEY=12345678901234567890123456789012`

**Auth tests skipped**
- Set TEST_CLERK_EMAIL and TEST_CLERK_PASSWORD in .env.e2e
- Use Clerk test mode: email like `e2e+clerk_test@domain.com`

**Tests marked with `.skip`**
- Some tests require authenticated user session
- Some tests require test data in database
- These are progressively enabled as infrastructure is built

## E2E Test Notes

- **With Docker**: Script auto-starts PostgreSQL container, runs migrations, passes DATABASE_URL to backend
- **Without Docker**: Backend runs in degraded mode (use `E2E_SKIP_POSTGRES=true` to skip explicitly)
- **Backend auto-build**: Script builds `backend/bin/server` if missing
- **Requires Atlas CLI**: Install via `brew install ariga/tap/atlas` (macOS) or https://atlasgo.io
- E2E tests mock API responses via `page.route()` for non-auth tests
- Dynamic ports avoid conflicts in parallel execution
