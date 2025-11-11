# E2E Test Failure Investigation - Phase 2A

**Date:** 2025-11-11
**Issue:** #331 E2E Test Scenarios Framework
**Status:** Root cause identified
**Investigator:** Claude Code

## Executive Summary

- **Root cause:** MockPlaidService returns hardcoded `AccountId: "mock-account-001"` but E2E scenarios create accounts with `plaid_account_id: "plaid-acc-free-001"` (different IDs)
- **Impact:** Transaction sync fails silently - transactions are "added" by Plaid but fail account mapping, resulting in 0 transactions stored
- **Pass rate:** 44% (14/32 tests) after Phase 1 UUID fixes - minimal improvement from 39% baseline
- **Fix required:** Update MockPlaidService.generateMockTransactions() to dynamically determine correct plaid_account_id from database

## Investigation Timeline

### Phase 1: UUID Conversion (Completed - Did Not Solve Issue)
**Commit:** 2df5bb3
**Changes:** Converted all string IDs to UUID format in 7 YAML scenario files
**Result:** Pass rate 39% → 44% (minimal improvement, likely test variance)
**Conclusion:** UUID mismatches were NOT the root cause

### Phase 2A: Deep Webhook Processing Investigation

#### 1. Webhook Flow Analysis
**File:** `frontend/e2e/lib/scenario-runner.ts` (lines 305-349)

**Finding 1: UUID Transformation Working Correctly**
- Line 308-319: `transformRequestBody()` converts scenario item_id to UUID format via `ensureUUID()`
- Line 328: Transformed body passed to API client
- ✅ This is working - admin endpoint receives valid UUID item_id

**Finding 2: Webhook Firing Flow**
- Scenarios fire webhook via `POST /api/v1/admin/plaid/fire-webhook`
- Body includes: `item_id` (UUID), `webhook_code` ("INITIAL_UPDATE", "DEFAULT_UPDATE")
- Expected: Admin handler → PlaidService.FireWebhook() → MockPlaidService returns mock transactions

#### 2. Plaid Service Investigation
**File:** `backend/internal/services/plaid_service.go` (lines 442-504)

**Finding 1: Real PlaidService.FireWebhook()**
- Lines 462-504: Calls Plaid sandbox API to fire webhook
- Returns `SandboxItemFireWebhookResponse` with `webhook_fired: true`
- ⚠️ This only TRIGGERS a webhook - does NOT directly return transactions
- The webhook is processed ASYNCHRONOUSLY by WebhookProcessor worker

**Finding 2: Real PlaidService.TransactionsSync()**
- Lines 401-440: Calls Plaid API `/transactions/sync`
- Returns `TransactionsSyncResponse` with Added/Modified/Removed arrays
- This is what actually fetches transactions (called by TransactionSyncService)

#### 3. Mock Service Analysis
**File:** `backend/internal/services/mock_plaid_service.go` (lines 106-213)

**Finding 1: CRITICAL BUG - Hardcoded Account ID**
```go
// Line 196: HARDCODED account ID that doesn't match E2E scenarios
accountId := "mock-account-001"
```

**E2E Scenario Expected:** `plaid_account_id: "plaid-acc-free-001"` (from YAML)
**Mock Returns:** `AccountId: "mock-account-001"` (hardcoded)
**Result:** Account mapping fails → transactions skipped → 0 transactions stored

**Finding 2: Mock TransactionsSync() Returns Transactions**
- Lines 106-136: Returns mock `TransactionsSyncResponse` with generated transactions
- Correctly respects `daysRequested` parameter (30 for free tier)
- ✅ Returns correct number of transactions (30 for free tier)
- ❌ But all have wrong AccountId → mapping fails

**Finding 3: Mock FireWebhook() is a No-Op**
- Lines 149-172: Just returns success response
- Does NOT actually trigger webhook processing
- Does NOT create transactions
- **This means E2E tests rely on WebhookProcessor worker to process the webhook**

#### 4. Webhook Processor Analysis
**File:** `backend/internal/workers/webhook_processor.go` (lines 98-142)

**Finding 1: Webhook Processing Flow**
- Lines 101-105: Fetches unprocessed TRANSACTIONS webhooks from database
- Line 128: Calls `syncService.ProcessWebhook()` for each webhook
- This triggers the full sync flow: fetch linked item → decrypt token → call TransactionsSync → store transactions

**Finding 2: Async Processing Timing**
- Webhook processor runs every 30 seconds (configurable interval)
- E2E tests wait 5 seconds for processing (line 71 in YAML scenario delay)
- ✅ 5 seconds should be enough if webhook is processed immediately
- ⚠️ But if webhook processor hasn't run yet, test will fail

#### 5. Transaction Sync Service Analysis
**File:** `backend/internal/services/transaction_sync_service.go` (lines 166-342)

**Finding 1: Account Mapping Logic**
- Lines 357-371: `buildAccountMap()` creates map of `plaid_account_id → internal UUID`
- Lines 388-391: `mapPlaidTransaction()` looks up account by `plaidTxn.AccountId`
- **CRITICAL:** If `plaidTxn.AccountId` not in map → error logged, transaction SKIPPED

**Finding 2: Graceful Error Handling Hides the Bug**
- Lines 522-527: When account mapping fails, transaction is skipped with warning
- Lines 532-535: If all transactions skipped, logs warning but returns success (no error)
- **This is why tests don't show explicit errors - transactions are silently discarded**

## Root Cause Determination

**THE DEFINITIVE ROOT CAUSE:**

The `MockPlaidService` generates transactions with a hardcoded `AccountId: "mock-account-001"`, but E2E scenarios create accounts with different `plaid_account_id` values like `"plaid-acc-free-001"`.

**Flow of Failure:**
1. ✅ Webhook is stored in database
2. ✅ WebhookProcessor picks it up
3. ✅ TransactionSyncService calls MockPlaidService.TransactionsSync()
4. ✅ Mock returns 30 transactions with `AccountId: "mock-account-001"`
5. ✅ TransactionSyncService builds account map: `{"plaid-acc-free-001" → UUID}`
6. ❌ **Account mapping fails** - "mock-account-001" not found in map
7. ⚠️ Transactions are **silently skipped** (logged as warnings, not errors)
8. ✅ Cursor is updated (sync marked successful even though 0 transactions stored)
9. ❌ **Tests fail** - database has 0 transactions, expected 30

**Why This Wasn't Caught Earlier:**
- Issue #331 UUID fixes focused on item_id matching, not account_id
- Graceful error handling in processAddedTransactions() hides the failure
- Logs show warnings, but tests don't fail with explicit errors
- Cursor update succeeds → tests see "successful sync" with wrong transaction count

## Proposed Fix

### Option A: Update MockPlaidService to Lookup Account IDs (RECOMMENDED)

Modify `MockPlaidService.TransactionsSync()` to:
1. Accept linked item repository as dependency
2. Use item_id from request to lookup linked item
3. Fetch accounts for this linked item
4. Use first account's `plaid_account_id` for generated transactions
5. Fallback to "mock-account-001" if no accounts found

**Advantages:**
- More realistic (mock behaves like real Plaid)
- Flexible for different test scenarios
- Doesn't require updating all YAML files
- Future-proof for scenarios with multiple accounts

**Code Changes Required:**
```go
// backend/internal/services/mock_plaid_service.go

type mockPlaidService struct {
    config         *config.Config
    accountRepo    repository.AccountRepository  // NEW
    linkedItemRepo repository.LinkedItemRepository  // NEW
}

func (m *mockPlaidService) TransactionsSync(ctx context.Context, accessToken string, ...) {
    // 1. Lookup linked item by encrypted access token
    // 2. Get accounts for this linked item
    // 3. Use first account's plaid_account_id
    // 4. Pass to generateMockTransactions()

    accounts, err := m.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
    accountId := "mock-account-001" // fallback
    if err == nil && len(accounts) > 0 {
        accountId = accounts[0].PlaidAccountID
    }

    return m.generateMockTransactions(days, accountId)
}
```

### Option B: Update All E2E Scenarios to Use "mock-account-001"

Change all YAML scenarios to use:
```yaml
plaid_account_id: "mock-account-001"
```

**Advantages:**
- Simpler fix (no code changes)
- Immediate fix

**Disadvantages:**
- Requires updating all 7 YAML files
- Less realistic testing
- Brittle (hardcoded dependency on mock implementation)
- Doesn't scale if we add more mock scenarios

### Recommendation: Option A

Option A is the better long-term solution because it makes the mock service behave more like the real Plaid API and doesn't create brittle dependencies between test data and mock implementation details.

## Secondary Issue: Webhook Processing Timing

**Problem:** E2E tests fire webhook and wait 5 seconds, but WebhookProcessor runs every 30 seconds.

**Potential race condition:** If processor just ran, next run could be 30 seconds away, causing test timeout.

**Suggested Enhancement:**
Add test helper endpoint: `POST /api/v1/admin/process-webhooks` (staging only)
- Immediately processes pending webhooks
- Returns count of webhooks processed
- Tests can call this instead of sleeping for 5 seconds
- More deterministic and faster test execution

## Test Pass/Fail Breakdown

### Passing Tests (14/32 - 44%)
- User notes preservation tests ✅
- Duplicate prevention tests ✅
- Orphaned data cleanup tests ✅
- Data integrity checks ✅

**Why these pass:** They don't depend on transaction syncing, only on data that's manually seeded in YAML scenarios.

### Failing Tests (18/32 - 56%)
- Transaction sync tests ❌
- Webhook processing tests ❌
- Tier limit enforcement tests ❌
- Transaction count assertions ❌

**Why these fail:** They all depend on MockPlaidService creating transactions that match the account IDs in the test scenarios.

## Expected Impact of Fix

**After implementing Option A:**
- Pass rate should improve from 44% to ~90-100%
- All transaction sync tests should pass
- Tier limit tests should pass
- Webhook processing tests should pass

**Remaining potential failures:**
- Timing-related issues (if webhook processor hasn't run yet)
- Other edge cases not yet discovered

## Files Involved

### Core Issue
1. `backend/internal/services/mock_plaid_service.go` - Contains hardcoded account ID
2. `backend/internal/services/transaction_sync_service.go` - Account mapping logic that fails silently
3. `frontend/e2e/scenarios/transaction-sync/*.yaml` - 7 scenario files with account definitions

### Related
4. `backend/cmd/server/main.go` - MockPlaidService initialization (may need to inject dependencies)
5. `backend/internal/workers/webhook_processor.go` - Async webhook processing
6. `backend/internal/repository/accounts.go` - Account lookup methods

## Next Steps

1. Implement Option A fix in MockPlaidService
2. Add account repository dependency to mock service
3. Update service initialization to inject repositories
4. Test locally with docker-compose
5. Push and verify CI passes
6. If timing issues persist, implement admin webhook processor endpoint
7. Update E2E scenarios to use immediate webhook processing

## Knowledge Preservation Notes

**For future context compaction:**
- The root cause was NOT UUID mismatches in YAML files
- The root cause WAS hardcoded account ID in mock service
- Graceful error handling can hide critical bugs
- Always check if mock services match test data structure
- Silent failures are dangerous - consider failing fast instead

**Key Learnings:**
- MockPlaidService needs to be data-driven, not hardcoded
- Test data (YAML scenarios) and mock services must stay in sync
- Account mapping is the critical step where mismatches cause silent failures
- Webhook processing is asynchronous - timing matters for E2E tests
