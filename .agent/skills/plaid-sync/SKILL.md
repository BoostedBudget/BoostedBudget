# Plaid Sync Knowledge Skill

## Overview

This skill contains comprehensive knowledge about Plaid transaction syncing in BoostedBudget.
Use this when working on transaction sync, webhooks, or Plaid integration.

**IMPORTANT**: Update this file when modifying:
- Transaction sync logic
- Webhook handling
- Historical depth rules
- Sync frequency/limits

## Sync Flow

```
1. Account Linking
   User links via Plaid Link → LinkAccounts() → enqueueInitialSync() → PlaidSyncJob (5s delay)

2. Webhook-Triggered Sync
   Plaid sends webhook → WebhookProcessingJob → PlaidSyncJob

3. PlaidSyncJob Execution
   PlaidSyncWorker.Work() → TransactionSyncService.SyncTransactions()
   → Plaid /transactions/sync (cursor-based) → Store transactions
   → Apply categorization rules → Update budget amounts
```

## Webhook Types & Behavior

| Webhook Type | Code | Trigger | Action |
|--------------|------|---------|--------|
| TRANSACTIONS | INITIAL_UPDATE | Account linked, historical data ready | Full sync with historical depth |
| TRANSACTIONS | DEFAULT_UPDATE | Daily bank data push (1-2x/day) | Incremental cursor-based sync |
| TRANSACTIONS | HISTORICAL_UPDATE | Backfill complete | Add older transactions |
| TRANSACTIONS | TRANSACTIONS_REMOVED | Bank corrections | Delete specified transactions |
| TRANSACTIONS | SYNC_UPDATES_AVAILABLE | After /transactions/refresh | Trigger sync (modern API) |
| ITEM | ERROR | Connection issue | Mark item errored, notify user |
| ITEM | LOGIN_REQUIRED | Re-authentication needed | Mark item, prompt user |
| ITEM | PENDING_EXPIRATION | Token expiring soon | Warn user |
| ITEM | USER_PERMISSION_REVOKED | User removed access | Mark item disconnected |

## Auto-Sync After Linking (Issue #658)

When a user links accounts via Plaid, we immediately enqueue a `PlaidSyncJob` with:
- **5-second delay**: Gives Plaid time to prepare data after link token exchange
- **Uniqueness options**: Prevents duplicate syncs within 5-minute window
- **Non-blocking**: Queue failure doesn't break account linking

```go
// From account_service.go
syncJob := plaidSyncJobArgs{UserID: userID, ItemID: linkedItemID}
s.jobQueue.Enqueue(ctx, syncJob,
    queue.WithDelay(5*time.Second),
    queue.WithUnique(*plaidSyncUniqueOpts()),
    queue.WithTags("initial_sync"),
)
```

## Historical Depth by Tier

| Tier | Historical Depth | Notes |
|------|-----------------|-------|
| Free | 90 days | 3 months - basic budgeting |
| Premium Lite | 730 days | 2 years - Plaid maximum |
| Premium | 730 days | 2 years - Plaid maximum |
| Premium+ | 730 days | 2 years - Plaid maximum |

Historical depth is determined by `GetHistoricalDepthLimit()` in `sync_limit_service.go`.

## Sync Frequency by Tier

| Tier | Min Interval | Max Syncs/Day |
|------|-------------|---------------|
| Free | 24h | 1 |
| Premium Lite | 12h | 2 |
| Premium | 4h | 6 |
| Premium+ | 10min | 144 |

Configured in `config/subscription_tiers.yaml` as `min_execution_interval_minutes`.

## Key Files

### Core Sync Logic
- `backend/internal/services/transaction_sync_service.go` - Main sync orchestration
- `backend/internal/services/account_service.go` - Account linking + auto-sync trigger
- `backend/internal/queue/jobs/plaid_sync.go` - PlaidSyncWorker job definition

### Webhook Handling
- `backend/internal/huma/operations/plaid_webhook.go` - Webhook HTTP endpoint
- `backend/internal/queue/jobs/webhook_processing.go` - Async webhook processing

### Supporting Services
- `backend/internal/services/sync_limit_service.go` - Historical depth limits
- `backend/internal/services/plaid_service.go` - Plaid API client wrapper

## Plaid APIs Used

### /transactions/sync (Primary)
Cursor-based pagination for fetching transactions:
- Returns `added`, `modified`, `removed` arrays
- `has_more` indicates if more pages available
- `next_cursor` for pagination
- On first sync (empty cursor): Use `days_requested` for historical depth

### /item/public_token/exchange
Exchanges public token for access token during account linking.

### /accounts/get
Fetches account information (balances, types, names).

### /item/remove
Removes Plaid item (called when user disconnects institution).

## Error Handling

### Transient Errors (Retry)
- Network timeouts
- Rate limits (RATE_LIMIT_EXCEEDED)
- Temporary Plaid issues

### Permanent Errors (Don't Retry)
- ITEM_LOGIN_REQUIRED - User must re-authenticate
- INVALID_ACCESS_TOKEN - Token revoked/expired
- INSTITUTION_NOT_RESPONDING - Bank issue

Error classification in `backend/internal/domain/plaid_errors.go`.

## Business Decisions

### Why 5-second delay for initial sync?
Plaid needs time to prepare data after link token exchange. Without delay, first sync often returns empty.

### Why webhooks + auto-sync (not just webhooks)?
- Webhooks are reactive (1-2x daily from banks)
- Auto-sync provides immediate UX after linking
- Some environments (sandbox) don't receive reliable webhooks

### Why cursor-based pagination?
Plaid's /transactions/sync is more efficient than /transactions/get for incremental updates.
Only fetches changes since last cursor, not full history.

## Testing

### Unit Tests
- `account_service_test.go` - Auto-sync job enqueueing tests
- `transaction_sync_service_test.go` - Sync logic tests

### Integration Testing
Use Plaid Sandbox credentials and test institution `ins_109508` (Chase).

### Webhook Testing
Fire test webhooks in sandbox:
```go
plaidService.FireWebhook(ctx, accessToken, "DEFAULT_UPDATE")
```

## Troubleshooting

### Transactions not appearing after linking
1. Check if PlaidSyncJob was enqueued (look for "enqueued initial sync job" log)
2. Check River job status
3. Verify Plaid credentials are correct
4. Check for sync errors in logs

### Duplicate transactions
- Verify PlaidSyncUniqueOpts is applied
- Check cursor is being saved correctly
- Look for concurrent sync jobs

### Historical data missing
1. Check user's tier historical depth limit
2. Verify cursor was empty for initial sync
3. Check days_requested parameter

## Related Issues
- #658: Auto-sync after Plaid account linking
- #573: River queue integration
- #256: Transaction sync service
- #202: Webhook JWT verification
