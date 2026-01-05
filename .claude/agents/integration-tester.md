# Integration Tester Subagent

Use this agent to test Clerk, Plaid, and Paddle integrations specifically.

## Description

Tests authentication flows (Clerk JWT validation), financial data sync (Plaid webhooks), and payment flows (Paddle subscription events). Includes environment-specific notes for QA and staging limitations.

## When to Use

Use when:
- Testing authentication flows
- Verifying Plaid webhook processing
- Testing subscription/payment flows
- Debugging integration issues

## Tools

- **Bash**: Run integration commands
- **Read**: Examine integration code and logs

## Model

sonnet (needs reasoning for complex integrations)

## Current Limitations

> **TODO: Staging Environment Setup**
>
> Some integrations are not fully usable from the QA environment:
> - **Plaid**: Requires production/sandbox credentials, not available in QA
> - **Paddle**: Webhook testing requires staging environment
> - **Clerk**: Works in all environments with proper test mode

## Instructions

### 1. Clerk Authentication Testing

Clerk works in all environments using test mode:

**Test User Format:**
- Email: `any_email+clerk_test@example.com`
- Verification code: `424242`

**Verify JWT Middleware:**
```bash
# Check Clerk config
grep -r "CLERK_SECRET_KEY" backend/.env* frontend/.env*

# Test JWT validation (requires valid token)
curl -H "Authorization: Bearer $TEST_JWT" http://localhost:8080/api/v1/profile
```

**Test Files:**
- `backend/internal/middleware/clerk.go` - JWT validation
- `backend/internal/middleware/clerk_test.go` - Middleware tests

### 2. Plaid Integration Testing

> **TODO: Not available in QA environment**
>
> Plaid testing requires sandbox credentials. For now, test using:
> - Unit tests in `backend/internal/services/plaid_service_test.go`
> - Mock responses in E2E tests

**When staging is available:**
```bash
# Test Plaid webhooks
curl -X POST http://localhost:8080/api/v1/webhooks/plaid \
  -H "Content-Type: application/json" \
  -d '{"webhook_type": "TRANSACTIONS", "webhook_code": "DEFAULT_UPDATE"}'
```

**Webhook Codes to Test:**
- `INITIAL_UPDATE` - First sync (historical depth based on tier)
- `DEFAULT_UPDATE` - Incremental sync
- `HISTORICAL_UPDATE` - Backfill transactions
- `TRANSACTIONS_REMOVED` - Retroactive deletions

### 3. Paddle Payment Testing

> **TODO: Requires staging environment**
>
> Paddle webhook testing needs proper staging setup with:
> - Sandbox Paddle account
> - Webhook URL configured
> - Test subscription plans

**When available:**
```bash
# Test subscription webhook
curl -X POST http://localhost:8080/api/v1/webhooks/paddle \
  -H "Content-Type: application/json" \
  -d '{"event_type": "subscription.activated", ...}'
```

### 4. Integration Test Commands

**Run integration tests (currently unit-level):**
```bash
cd backend && go test -tags=integration ./...
```

**Check integration health:**
```bash
# Clerk
curl -s http://localhost:8080/health | jq '.clerk'

# Database
curl -s http://localhost:8080/health | jq '.database'
```

## Files Reference

| Integration | Key Files |
|-------------|-----------|
| Clerk | `backend/internal/middleware/clerk.go`, `backend/internal/auth/claims.go` |
| Plaid | `backend/internal/services/plaid_service.go`, `backend/internal/handlers/plaid_handler.go` |
| Paddle | `backend/internal/services/subscription_service.go` |
| Webhooks | `backend/internal/workers/webhook_processor.go` |

## Future Work

When staging environment is fully set up:
- [ ] Add Plaid sandbox testing
- [ ] Add Paddle webhook testing
- [ ] Add end-to-end subscription flow tests
- [ ] Add transaction sync verification tests

## Related Skills

- `.claude/skills/integration-guides/` - Integration implementation details
