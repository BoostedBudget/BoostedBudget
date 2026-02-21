---
name: plaid-staging-testing
description: "Plaid staging validation for BoostedBudget: connectivity checks, sync validation, webhook testing, and error handling verification."
---

# Plaid Staging Testing Guide

## Overview

This skill provides tools and patterns for validating Plaid integration in staging environments.
Use this when testing Plaid connectivity, transaction sync, webhook handling, or debugging
integration issues.

**Key Components:**
- Validation CLI at `backend/cmd/plaid_validation/`
- Test fixtures at `backend/testdata/plaid/`
- MockPlaidService for unit testing

## Quick Start

### Run All Validations

```bash
cd backend
go run ./cmd/plaid_validation/...
```

### Run Specific Scenarios

```bash
# Connectivity only (fast check)
go run ./cmd/plaid_validation/... connectivity

# Sync validation
go run ./cmd/plaid_validation/... sync-initial sync-incremental

# All validations with verbose output
go run ./cmd/plaid_validation/... --verbose all
```

### Dry-Run Mode

All validations are API-only by default (no database mutations):

```bash
go run ./cmd/plaid_validation/... all
```

### JSON Output for CI/CD

```bash
go run ./cmd/plaid_validation/... --json all > results.json
```

## Required Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `PLAID_CLIENT_ID` | Yes | Plaid client ID |
| `PLAID_SECRET` | Yes | Plaid secret for environment |
| `PLAID_ENV` | Yes | `sandbox`, `development`, or `production` |
| `PLAID_WEBHOOK_URL` | Recommended | Webhook endpoint URL |
| `PLAID_REDIRECT_URI` | Recommended | OAuth redirect URI |

## Validation Scenarios

### connectivity

Verifies Plaid API authentication works by creating a test link token.

**Checks:**
- `PLAID_CLIENT_ID` configured
- `PLAID_SECRET` configured
- API authentication succeeds
- Environment correctly detected (sandbox/development/production)

**Example output:**
```
[PASS] connectivity - Plaid API connection verified (142ms)
       plaid_env: sandbox
       webhook_url_configured: true
```

### link-token

Creates and validates a Plaid Link token.

**Checks:**
- Link token creation succeeds
- Token has valid expiration
- Request ID returned

### sync-initial

Simulates an initial transaction sync (empty cursor).

**Checks:**
- TransactionsSync API works with empty cursor
- Cursor pagination (has_more loop)
- Transaction response parsing
- Historical depth parameter respected

### sync-incremental

Simulates an incremental sync (with existing cursor).

**Checks:**
- Cursor-based continuation
- Modified transaction handling
- Removed transaction handling

### sync-pagination

Tests multi-page sync scenarios.

**Checks:**
- Multiple pages (3+) with has_more=true
- Cursor updates after each page
- Correct termination when has_more=false

### error-classification

Verifies error classification for different Plaid errors.

**Checks:**
- `INVALID_ACCESS_TOKEN` classified as permanent (no retry)
- `RATE_LIMIT_EXCEEDED` classified as rate limit (exponential backoff)
- Network errors classified as temporary (retry with backoff)
- `ITEM_LOGIN_REQUIRED` classified as permanent (user action needed)

### webhook-fire (sandbox only)

Fires a test webhook using Plaid sandbox API.

**Checks:**
- FireWebhook API works
- `webhook_fired` response received
- Only available in sandbox environment

## Test Fixtures

Located at `backend/testdata/plaid/`:

```
testdata/plaid/
├── fixtures.go              # Go embed + loaders
├── webhooks/
│   ├── transactions/        # TRANSACTIONS webhook payloads
│   │   ├── default_update.json
│   │   ├── initial_update.json
│   │   ├── historical_update.json
│   │   ├── sync_updates_available.json
│   │   └── transactions_removed.json
│   └── item/                # ITEM webhook payloads
│       ├── error.json
│       ├── login_required.json
│       ├── pending_expiration.json
│       └── user_permission_revoked.json
├── responses/
│   └── transactions_sync/   # TransactionsSync API responses
│       ├── single_page.json
│       ├── multi_page_1.json
│       ├── multi_page_2.json
│       ├── multi_page_final.json
│       ├── empty_response.json
│       └── large_batch.json
└── errors/                  # Plaid error responses
    ├── rate_limit_exceeded.json
    ├── invalid_access_token.json
    ├── item_login_required.json
    └── internal_server_error.json
```

### Using Fixtures in Tests

```go
import plaidtestdata "github.com/maxnussbaum/BoostedBudget/testdata/plaid"

// Load a sync response
resp := plaidtestdata.MustLoadTransactionsSyncResponse("single_page")

// Load webhook payload
payload, err := plaidtestdata.LoadWebhookPayload("transactions", "default_update")

// Load error fixture
plaidErr, err := plaidtestdata.LoadPlaidError("rate_limit_exceeded")
```

## MockPlaidService Usage

For unit tests, use MockPlaidService instead of real Plaid API calls:

```go
import (
    "github.com/maxnussbaum/BoostedBudget/internal/services"
    plaidtestdata "github.com/maxnussbaum/BoostedBudget/testdata/plaid"
)

func TestTransactionSync(t *testing.T) {
    mockPlaid := new(services.MockPlaidService)

    // Setup expectations
    mockPlaid.On("TransactionsSync", ctx, "access-token", "", 90).
        Return(plaidtestdata.MustLoadTransactionsSyncResponse("single_page"), nil)

    // Inject into service
    syncService := services.NewTransactionSyncService(
        // ... other deps
        mockPlaid,
        // ...
    )

    // Test
    result, err := syncService.SyncTransactions(ctx, linkedItem)

    // Assert
    assert.NoError(t, err)
    assert.Len(t, result.Added, 2)
    mockPlaid.AssertExpectations(t)
}
```

### Available Mock Methods

The MockPlaidService in `backend/internal/services/account_service_test.go` implements all PlaidService methods for testing. All mock methods follow testify/mock patterns for setup and verification.

## Cursor Persistence

### Critical Design

Cursor is updated **after each page** (not at end) for resumability:

```
NULL → first sync (cursor="") → page 1 cursor → page 2 cursor → ... → final cursor
       ↓ (on failure)
       Cursor stays at last successful page (resumable)
```

### Cursor Auto-Reset

On `INVALID_CURSOR` errors, the system automatically resets cursor to empty string
and triggers a full re-sync:

```go
// Automatic recovery - no manual intervention needed
if isInvalidCursorError(err) {
    logger.Warn("Invalid cursor detected, resetting for full re-sync")
    linkedItemRepo.UpdateCursor(ctx, linkedItem.ID, "")
    // Next retry will start fresh
}
```

### Testing Cursor Behavior

Key test cases to verify cursor handling:

| Test | What it Verifies |
|------|------------------|
| `TestSyncTransactions_CursorPersistsAfterTxnStorageFailure` | Cursor saved even if transaction storage fails |
| `TestSyncTransactions_CursorResumeFromLastSuccessfulPage` | Retry resumes from cursor position |
| `TestSyncTransactions_NilVsEmptyCursor` | Both nil and "" trigger first sync |
| `TestSyncTransactions_FinalPageCursorUpdate` | Cursor updated even on final page |

## Concurrent Sync Prevention

The system uses PostgreSQL `FOR UPDATE` row-level locking to prevent
concurrent syncs for the same linked item:

```go
// In PlaidSyncWorker.Work():
tx, _ := db.Begin(ctx)
defer tx.Rollback(ctx)

// Acquire exclusive lock - blocks if another worker has it
linkedItem, err := repo.GetByIDForUpdate(ctx, tx, itemID)
if err != nil {
    return err // Another worker may have it locked
}

// Proceed with sync...
result, err := syncService.SyncTransactions(ctx, linkedItem)

tx.Commit(ctx) // Releases lock
```

## Troubleshooting

### "Plaid API authentication failed"

1. Verify `PLAID_CLIENT_ID` and `PLAID_SECRET` are set
2. Check credentials match the `PLAID_ENV` (sandbox vs production have different secrets)
3. Verify API access is enabled in Plaid Dashboard
4. Check for typos or extra whitespace in environment variables

### "Link token creation failed"

1. Check `PLAID_REDIRECT_URI` is configured
2. Verify redirect URI is registered in Plaid Dashboard
3. Verify account has TRANSACTIONS product enabled
4. Check for correct environment (sandbox URIs differ from production)

### "Webhook not firing"

1. FireWebhook only works in **sandbox** environment
2. Verify access token is valid and not expired
3. Check webhook URL is publicly reachable from Plaid's servers
4. Verify webhook endpoint returns 200 OK

### "Sync returns empty"

1. Normal for sandbox accounts with no seeded transactions
2. Try using different sandbox test institutions (e.g., `ins_109508` for Chase)
3. Check if cursor is valid - corrupted cursor may skip all data
4. Verify historical depth limit isn't restricting results

### "Cursor appears stuck"

1. Check for permanent Plaid errors (ITEM_LOGIN_REQUIRED, INVALID_ACCESS_TOKEN)
2. Look for `INVALID_CURSOR` errors - should auto-reset
3. Verify linked item status isn't `error` or `login_required`
4. Check retry_count hasn't exceeded max attempts

### "Duplicate transactions appearing"

1. Check for concurrent sync jobs (look for race conditions in logs)
2. Verify `ON CONFLICT DO NOTHING` constraint is working
3. Ensure cursor is being updated after each successful page
4. Check job uniqueness window (5-minute deduplication)

## Related Resources

- [plaid-sync skill](./../plaid-sync/SKILL.md) - Transaction sync patterns and business logic
- [go-patterns skill](./../go-patterns/SKILL.md) - Service layer and repository patterns
- [queueing skill](./../queueing/SKILL.md) - PlaidSyncWorker job patterns and River queue
- [integration-guides skill](./../integration-guides/SKILL.md) - External service integration patterns

## Key Files Reference

### Validation CLI
- `backend/cmd/plaid_validation/main.go` - CLI entry point
- `backend/cmd/plaid_validation/validators/` - Individual validators

### Core Plaid Integration
- `backend/internal/services/plaid_service.go` - Plaid API client wrapper
- `backend/internal/services/transaction_sync_service.go` - Sync orchestration
- `backend/internal/services/account_service.go` - Account linking

### Job Processing
- `backend/internal/queue/jobs/plaid_sync.go` - PlaidSyncWorker
- `backend/internal/queue/jobs/webhook_processing.go` - WebhookProcessingWorker

### Data Layer
- `backend/internal/repository/linked_item_repository.go` - Cursor storage
- `backend/internal/domain/linked_item.go` - LinkedItem model
- `backend/internal/domain/transaction_sync.go` - Error classification

### Testing
- `backend/internal/services/mock_plaid_service.go` - Complete mock
- `backend/testdata/plaid/` - Test fixtures
