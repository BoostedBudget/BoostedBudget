# Issue #353: Balance Tracking Dashboard - Implementation Progress

**Status:** 🚧 In Progress
**Started:** 2025-11-15
**Current Phase:** Phase 1 - Database Migration & Repository
**Current TDD Cycle:** RED (creating tracking document)

---

## Quick Reference

### Architecture Decision: Balance Snapshots
✅ Store daily balance snapshots in database (not calculated from transactions)
✅ Capture during Plaid webhook processing (INITIAL_UPDATE, DEFAULT_UPDATE)
✅ One snapshot per account per day (UNIQUE constraint with UPSERT)
✅ Track history from account creation forward only

### Key Integration Point
**File:** `backend/internal/services/transaction_sync_service.go`
**Method:** `SyncTransactions()` - Add snapshot creation after transaction processing (line ~292)

---

## Implementation Log

### 2025-11-15 - Project Initialization

#### TDD Cycle 1: Database Migration
- **Status:** ✅ Complete
- Created migration 000016_create_balance_snapshots_table.up.sql
- Created rollback migration 000016_create_balance_snapshots_table.down.sql
- **Schema Features:**
  - Daily snapshots with UNIQUE(account_id, snapshot_date) constraint
  - Cascade deletes for GDPR compliance
  - Backfill query for existing accounts
  - Four indexes for query optimization
- Migration validated and tested

#### TDD Cycle 2: Domain Model
- **Status:** ✅ Complete
- Created `backend/internal/domain/balance_snapshot.go`
- **Model Features:**
  - BalanceSnapshot struct with validation
  - Source constants (plaid_webhook, migration, manual)
  - BalanceHistoryPoint for aggregated dashboard data
  - Validation ensures at least one balance field present

#### TDD Cycle 3: Repository Tests (RED)
- **Status:** ✅ Complete
- Created `backend/internal/repository/balance_snapshot_repository_test.go`
- **Tests Written:**
  - CreateOrUpdate with new snapshot
  - CreateOrUpdate with conflict (UPSERT behavior)
  - GetByUserIDAndDateRange with results
  - GetByUserIDAndDateRange with no results
  - GetAggregatedHistory with account type filter
  - GetAggregatedHistory without filter (all types)
  - GetLatestByUserID (DISTINCT ON pattern)
  - DeleteByAccountID
- Used sqlmock for database mocking
- **Result:** Tests initially failed as expected (RED phase ✅)

#### TDD Cycle 4: Repository Implementation (GREEN)
- **Status:** ✅ Complete
- Created `backend/internal/repository/balance_snapshot_repository.go`
- **Implementation Features:**
  - UPSERT with ON CONFLICT (account_id, snapshot_date) DO UPDATE
  - Conditional queries for optional account type filtering
  - DISTINCT ON for latest snapshots per account
  - Date truncation for daily granularity
  - pq.Array for PostgreSQL array parameters
- **Test Results:** All 8 tests passing ✅
- **Result:** GREEN phase complete ✅

### PHASE 1 COMPLETE ✅

**Summary:**
- ✅ Migration files created (up + down with rollback)
- ✅ Domain model with validation
- ✅ Repository with comprehensive test coverage
- ✅ All tests passing

### PHASE 2: Webhook Integration

#### Implementation: Balance Snapshot Creation
- **Status:** ✅ Complete
- Modified `TransactionSyncService` to capture balance snapshots
- **Integration Point:** After transaction processing completes (line 303)
- **Method Added:** `createBalanceSnapshots(ctx, linkedItem, accessToken)`
- **Features:**
  - Calls Plaid `/accounts/get` to fetch current balances
  - Maps Plaid accounts to internal UUIDs
  - Creates snapshots with daily deduplication (UPSERT)
  - Graceful error handling (logs warnings, doesn't fail sync)
  - Skips accounts without balance data
  - Supports multiple currencies via ISO currency code
- **Dependencies Updated:**
  - Added `balanceSnapshotRepo` to service struct
  - Updated constructor signature
  - Updated `main.go` to inject repository
  - Code compiles successfully ✅

**Decision Log:**
- Non-blocking implementation: Balance snapshot failures don't fail transaction sync
- Will retry on next webhook (daily deduplication ensures correctness)
- Used GetClient() pattern to call Plaid API (follows existing codebase patterns)

### PHASE 2 COMPLETE ✅

**Summary:**
- ✅ Balance snapshots captured on every Plaid webhook
- ✅ Integrated into TransactionSyncService
- ✅ Daily deduplication with UPSERT
- ✅ Non-critical error handling
- ✅ Code compiles and ready for testing

**Next:** Phase 3 - Dashboard API (Service + Handler + Routes)

### PHASE 3: Dashboard API

#### TDD Cycle 5: Dashboard Domain Types
- **Status:** ✅ Complete
- Created `backend/internal/domain/dashboard.go`
- **Types Created:**
  - BalanceSummaryResponse - Current balance overview with breakdown by account type
  - BalanceHistoryResponse - Time series data for charts
  - AccountTypesResponse - Available account types for filters
  - DashboardFilters - Query parameters for filtering
  - Supporting types: AccountTypeBalance, AccountBalanceDetail, PeriodComparisonData, BalanceHistorySnapshot, AccountTypeInfo

#### TDD Cycle 6: DashboardService (GREEN, then Tests)
- **Status:** ✅ Complete
- **Note:** Implementation created before tests (not ideal TDD, but tests written retroactively)
- Created `backend/internal/services/dashboard_service.go`
- Created `backend/internal/services/dashboard_service_test.go`
- **Service Methods:**
  1. GetBalanceSummary - Current balance with type breakdown, period comparison (vs 30 days ago)
  2. GetBalanceHistory - Time series data filtered by date range and account types
  3. GetAccountTypes - Available account types with counts and totals
- **Tests Written (8 total):**
  - TestGetBalanceSummary_Success
  - TestGetBalanceSummary_WithAccountTypeFilter
  - TestGetBalanceSummary_SnapshotRepoError
  - TestGetBalanceHistory_Success
  - TestGetBalanceHistory_InvalidDateRange
  - TestGetBalanceHistory_WithAccountTypeFilter
  - TestGetAccountTypes_Success
  - TestGetAccountTypes_NoSnapshots
- **Test Results:** All 8 tests passing ✅
- **Helpers:** formatAccountTypeLabel() - Maps account types to user-friendly labels

**Next:** Dashboard Handler tests (RED), then implementation (GREEN), then routes

#### TDD Cycle 7: DashboardHandler (RED-GREEN)
- **Status:** ✅ Complete
- Created `backend/internal/handlers/dashboard_handler_test.go`
- Created `backend/internal/handlers/dashboard_handler.go`
- **Tests Written (8 total):**
  - TestGetBalanceSummary_Success
  - TestGetBalanceSummary_WithQueryParams
  - TestGetBalanceSummary_Unauthorized
  - TestGetBalanceSummary_ServiceError
  - TestGetBalanceHistory_Success
  - TestGetBalanceHistory_InvalidDateFormat
  - TestGetAccountTypes_Success
  - TestGetAccountTypes_Unauthorized
- **Test Results:** All 8 tests passing ✅
- **Handler Features:**
  - User authentication via context (Clerk middleware)
  - Query parameter parsing (account_types, as_of_date, start_date, end_date, granularity)
  - Error handling (400 Bad Request, 401 Unauthorized, 500 Internal Server Error)
  - JSON response formatting

#### Routes Registration
- **Status:** ✅ Complete
- Updated `backend/cmd/server/main.go`:
  - Created dashboardService instance (line 123)
  - Created dashboardHandler instance (line 255)
  - Registered 3 dashboard routes (lines 290-292):
    - GET /api/v1/dashboard/balance-summary
    - GET /api/v1/dashboard/balance-history
    - GET /api/v1/dashboard/account-types
- **Verification:** Backend compiles successfully ✅

### PHASE 3 COMPLETE ✅

**Summary:**
- ✅ Dashboard domain types (requests & responses)
- ✅ DashboardService with comprehensive tests (8/8 passing)
- ✅ DashboardHandler with comprehensive tests (8/8 passing)
- ✅ Routes registered and protected with Clerk middleware
- ✅ Backend compiles successfully

**Files Created:**
- `backend/internal/domain/dashboard.go`
- `backend/internal/services/dashboard_service.go`
- `backend/internal/services/dashboard_service_test.go`
- `backend/internal/handlers/dashboard_handler.go`
- `backend/internal/handlers/dashboard_handler_test.go`

**Files Modified:**
- `backend/cmd/server/main.go` (dashboard service, handler, and routes)
- `backend/internal/services/dashboard_service_test.go` (removed duplicate mocks)
- `backend/internal/services/transaction_mapper_test.go` (fixed NewTransactionSyncService calls)
- `backend/internal/services/transaction_sync_service_test.go` (fixed NewTransactionSyncService calls)

**Next:** Phase 4 - Frontend Dashboard (React components, hooks, chart library integration)

#### Planning Phase
- **Completed:** Comprehensive codebase exploration using Plan subagent
- **Found:** Plaid webhook integration in `plaid_webhook_handler.go`
- **Found:** Transaction sync service processes webhooks via background `WebhookProcessor`
- **Found:** Current gap - balances only fetched during initial account linking, NOT during transaction syncs
- **Decision:** Add balance snapshot creation to `TransactionSyncService.SyncTransactions()` method
- **Rationale:** Reuses existing Plaid API access, minimal code duplication, time-aligned with transactions

#### Todo List Created
- Created comprehensive 33-task todo list covering all 5 phases
- Estimated timeline: 5-7 days
- Approach: Strict TDD (Red-Green-Refactor) throughout

#### Tracking Document Created
- **Status:** ✅ Complete
- Created `IMPLEMENTATION_PROGRESS.md` for context preservation across potential compactions
- Will be updated after EVERY TDD cycle

---

## Design Decisions

### 1. Database Schema - Daily Snapshots with Deduplication

**Decision:** Use `snapshot_date DATE` column with UNIQUE constraint
**Schema:**
```sql
CREATE TABLE balance_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID REFERENCES accounts(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    current_balance DECIMAL(15,2),
    available_balance DECIMAL(15,2),
    currency_code VARCHAR(3) DEFAULT 'USD',
    snapshot_date DATE NOT NULL,
    source VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT unique_account_date UNIQUE (account_id, snapshot_date)
);
```

**Rationale:**
- Daily granularity sufficient for financial dashboard
- UNIQUE constraint prevents duplicate snapshots same day
- ON CONFLICT UPDATE keeps latest balance if multiple webhooks fire
- Simpler queries than timestamp-based snapshots

**Alternative Considered:** TIMESTAMP column with no deduplication
**Trade-off:** More storage, more complex dashboard queries vs. full audit trail
**Choice:** Daily snapshots for simplicity

### 2. Webhook Integration Point

**Decision:** Add to `TransactionSyncService.SyncTransactions()` after transaction processing
**Location:** Line ~292 in `transaction_sync_service.go`

**Rationale:**
- Already has decrypted access token
- Already processes Plaid webhooks in background
- Natural time alignment with transaction data
- Non-blocking (runs in WebhookProcessor worker)

**Alternative Considered:** Separate balance sync worker
**Trade-off:** More resilient vs. more complex, duplicate API calls
**Choice:** Integrate with transaction sync for simplicity

### 3. Error Handling Strategy

**Decision:** Log warnings but don't fail transaction sync
**Pattern:**
```go
if err := s.createBalanceSnapshots(ctx, linkedItem, accessToken); err != nil {
    log.Printf("Warning: Failed to create balance snapshots: %v", err)
    // Don't return error - snapshots are non-critical for MVP
}
```

**Rationale:**
- Balance snapshots are enhancement, not critical for transaction sync
- Failed snapshot doesn't break user's transaction data
- Will retry on next webhook (daily updates)

**Future Enhancement:** Add retry queue for failed snapshots

### 4. Frontend Gap Filling

**Decision:** Forward-fill strategy for days without snapshots
**Algorithm:** Each day inherits previous day's balance until new snapshot arrives

**Rationale:**
- Creates smooth, continuous chart
- Honest representation (balance likely didn't change)
- Common pattern in financial dashboards

**Alternative Considered:** Linear interpolation
**Trade-off:** Smoother chart vs. potentially misleading data
**Choice:** Forward-fill with visual markers for actual snapshot dates

---

## Tests Written

### Backend Tests
- [ ] `balance_snapshot_repository_test.go::TestCreateOrUpdate_Success`
- [ ] `balance_snapshot_repository_test.go::TestCreateOrUpdate_Conflict`
- [ ] `balance_snapshot_repository_test.go::TestGetAggregatedHistory`
- [ ] `balance_snapshot_repository_test.go::TestGetByUserIDAndDateRange`
- [ ] `transaction_sync_service_test.go::TestCreateBalanceSnapshots_Success`
- [ ] `transaction_sync_service_test.go::TestCreateBalanceSnapshots_PlaidAPIError`
- [ ] `dashboard_service_test.go::TestGetBalanceSummary`
- [ ] `dashboard_service_test.go::TestGetBalanceHistory`
- [ ] `dashboard_handler_test.go::TestGetBalanceSummary_Success`
- [ ] `dashboard_handler_test.go::TestGetBalanceSummary_Unauthorized`

### Frontend Tests
- [ ] `dashboardApi.test.ts::getBalanceSummary`
- [ ] `dashboardApi.test.ts::getBalanceHistory`
- [ ] `useDashboardData.test.tsx::fetches data on mount`
- [ ] `useDashboardData.test.tsx::fills gaps in snapshot data`
- [ ] `BalanceHistoryChart.test.tsx::renders chart with data`
- [ ] `BalanceHistoryChart.test.tsx::handles zoom interaction`
- [ ] `DashboardPage.test.tsx::loads and displays dashboard`

### E2E Tests
- [ ] `dashboard.spec.ts::authenticated user sees dashboard`
- [ ] `dashboard.spec.ts::filters by account type`
- [ ] `dashboard.spec.ts::changes timeframe`
- [ ] `dashboard.spec.ts::zooms chart`

**Target Coverage:** >80% (backend + frontend)

---

## Blockers

**Current:** None

**Resolved:** None yet

---

## Performance Metrics

**Target Metrics:**
- Balance summary API: <300ms
- Balance history API: <500ms
- Frontend initial load: <2000ms
- Chart rendering: 60fps

**Actual Metrics:** (TBD - measure during testing phase)
- Balance summary: N/A
- Balance history: N/A
- Frontend load: N/A

---

## Migration Status

**Migration Number:** 000XXX (will be determined)
**Status:** Not created yet
**Idempotency:** ✅ Planned (CREATE IF NOT EXISTS, ON CONFLICT DO NOTHING)
**Rollback:** ✅ Planned (DROP TABLE IF EXISTS CASCADE)
**Backfill:** ✅ Planned (insert from existing accounts.current_balance)

---

## Next Steps

1. ✅ Create IMPLEMENTATION_PROGRESS.md
2. ✅ Create migration files (up.sql + down.sql)
3. ✅ Create BalanceSnapshot domain model
4. ⏭️ RED: Write BalanceSnapshotRepository tests ← **CURRENT**
5. ⏭️ GREEN: Implement BalanceSnapshotRepository
6. ⏭️ REFACTOR: Optimize repository queries

---

## Files Created

### Backend
- [x] `backend/migrations/000016_create_balance_snapshots_table.up.sql`
- [x] `backend/migrations/000016_create_balance_snapshots_table.down.sql`
- [x] `backend/internal/domain/balance_snapshot.go`
- [ ] `backend/internal/repository/balance_snapshot_repository.go`
- [ ] `backend/internal/repository/balance_snapshot_repository_test.go`
- [ ] `backend/internal/services/dashboard_service.go`
- [ ] `backend/internal/services/dashboard_service_test.go`
- [ ] `backend/internal/handlers/dashboard_handler.go`
- [ ] `backend/internal/handlers/dashboard_handler_test.go`
- [ ] `backend/internal/domain/dashboard.go`

### Frontend
- [ ] `apps/web/src/features/dashboard/` (entire directory)
- [ ] `apps/web/e2e/dashboard.spec.ts`

### Tracking
- [x] `IMPLEMENTATION_PROGRESS.md` ← **CURRENT FILE**

---

## Files Modified

### Backend
- [ ] `backend/internal/services/transaction_sync_service.go` (add createBalanceSnapshots method)
- [ ] `backend/internal/services/transaction_sync_service_test.go` (add tests)
- [ ] `backend/cmd/server/main.go` (register dashboard routes)

### Frontend
- [ ] `apps/web/src/App.tsx` (conditional routing)
- [ ] `apps/web/package.json` (dependencies)

---

## Knowledge & Gotchas

### Plaid API Patterns
- **Access Token:** Must decrypt before using with Plaid API (encryption service)
- **Account Fetching:** Use `plaid.NewAccountsGetRequest(accessToken)` pattern (see `account_service.go:149`)
- **Balance Fields:** `balances.GetCurrentOk()` returns `*float64` - check nil before dereferencing
- **Error Handling:** Plaid errors wrapped in `PlaidError` type with `error_code` field

### Webhook Processing
- **Async:** Webhooks processed by background `WebhookProcessor` (30s poll interval)
- **Batching:** Processes up to 10 webhooks per type per batch
- **Retry:** No automatic retry - will process on next poll if failed
- **Idempotency:** Webhook can be processed multiple times - use UPSERT for snapshots

### Database Patterns
- **UUID Generation:** Use `uuid.New().String()` (not database default)
- **Timestamps:** Use `time.Now()` in Go, not database DEFAULT
- **Cascading Deletes:** ON DELETE CASCADE ensures GDPR compliance (user deletion removes all data)
- **Indexing:** Always index foreign keys and date columns for query performance

### Testing Patterns
- **Repository Tests:** Use in-memory SQLite or test database (follow existing pattern)
- **Service Tests:** Mock repository dependencies
- **Handler Tests:** Use Echo test context (`setupTestContext` helper)
- **E2E Tests:** Playwright with Clerk authentication flow

---

## Lessons Learned

(Will be populated during implementation)

---

## Future Enhancements

(Out of scope for this PR, tracked separately)

- Redis caching for dashboard queries
- Intraday balance snapshots (hourly)
- Balance anomaly detection (unusual changes)
- Retry queue for failed snapshot creation
- Balance snapshot comparison (daily/weekly/monthly views)
- Export balance history to CSV

---

**Last Updated:** 2025-11-15 (Domain model created)
**Next Update:** After repository tests written
