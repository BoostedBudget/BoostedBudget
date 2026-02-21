---
description: Health-check third-party service integrations (Clerk, Plaid, Paddle).
---

# Check Integrations

Health-check third-party service integrations (Clerk, Plaid, Paddle).

## Usage

```
/check-integrations [service]
```

Examples:
- `/check-integrations` - Check all integrations
- `/check-integrations clerk` - Check Clerk only

## Current Status

> **TODO: Staging Environment Setup Required**
>
> Full integration checking requires staging environment. Current checks are limited:
> - **Clerk**: Available in all environments
> - **Plaid**: Requires sandbox/production credentials (not in QA)
> - **Paddle**: Requires staging setup

## Available Checks

### 1. Clerk Authentication

**Always available:**

```bash
# Check environment variables
grep -E "CLERK_(SECRET|PUBLISHABLE)_KEY" backend/.env apps/web/.env

# Test health endpoint (if backend running)
curl -s http://localhost:8080/health | jq '.services.clerk // "not available"'

# Verify JWT template exists in Clerk dashboard
# Template name: "BoostedBudget"
# Required claims: user_id, email, subscription_tier, primary_region
```

**Test JWT validation:**
```bash
# Get test token from Clerk dashboard or test user
export TEST_TOKEN="eyJ..."

# Test protected endpoint
curl -H "Authorization: Bearer $TEST_TOKEN" \
  http://localhost:8080/api/v1/profile
```

### 2. Plaid Financial Data

> **TODO: Not available in QA environment**

**When staging available:**
```bash
# Check Plaid credentials
grep -E "PLAID_(CLIENT_ID|SECRET|ENV)" backend/.env

# Test health
curl -s http://localhost:8080/health | jq '.services.plaid'

# Test link token creation (requires auth)
curl -H "Authorization: Bearer $TOKEN" \
  -X POST http://localhost:8080/api/v1/plaid/link-token
```

**Sandbox testing:**
- Use Plaid's sandbox credentials
- Test institution: `ins_109508` (Chase)
- Test credentials: `user_good` / `pass_good`

### 3. Paddle Payments

> **TODO: Requires staging environment**

**When available:**
```bash
# Check Paddle credentials
grep -E "PADDLE_(VENDOR_ID|API_KEY)" backend/.env

# Test health
curl -s http://localhost:8080/health | jq '.services.paddle'

# Test subscription status
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/subscription/status
```

### 4. Database

**Always available:**
```bash
# Check database connection
curl -s http://localhost:8080/health | jq '.services.database'

# Direct connection test
psql "$DATABASE_URL" -c "SELECT 1"
```

## Integration Status Report

Run all available checks:

```bash
echo "=== Integration Status ==="

echo -n "Database: "
curl -s http://localhost:8080/health | jq -r '.services.database.status // "unknown"'

echo -n "Clerk: "
curl -s http://localhost:8080/health | jq -r '.services.clerk.status // "unknown"'

echo -n "Plaid: "
curl -s http://localhost:8080/health | jq -r '.services.plaid.status // "not configured"'

echo -n "Paddle: "
curl -s http://localhost:8080/health | jq -r '.services.paddle.status // "not configured"'
```

## Environment Requirements

| Service | Required Environment Variables |
|---------|-------------------------------|
| Clerk | `CLERK_SECRET_KEY`, `CLERK_PUBLISHABLE_KEY` |
| Plaid | `PLAID_CLIENT_ID`, `PLAID_SECRET`, `PLAID_ENV` |
| Paddle | `PADDLE_VENDOR_ID`, `PADDLE_API_KEY` |
| Database | `DATABASE_URL` |

## Future Work

When staging is fully set up:
- [ ] Automated Plaid sandbox tests
- [ ] Paddle webhook verification
- [ ] End-to-end subscription flow tests
- [ ] Integration health dashboard

## Related

- `.claude/skills/integration-guides/` - Integration implementation details
- `.claude/agents/integration-tester.md` - Integration testing agent
