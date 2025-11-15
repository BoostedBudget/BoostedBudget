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
- Regenerated Atlas checksums

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

**Next:** Phase 2 - Webhook Integration

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
- [ ] `frontend/src/features/dashboard/` (entire directory)
- [ ] `frontend/e2e/dashboard.spec.ts`

### Tracking
- [x] `IMPLEMENTATION_PROGRESS.md` ← **CURRENT FILE**

---

## Files Modified

### Backend
- [ ] `backend/internal/services/transaction_sync_service.go` (add createBalanceSnapshots method)
- [ ] `backend/internal/services/transaction_sync_service_test.go` (add tests)
- [ ] `backend/cmd/server/main.go` (register dashboard routes)

### Frontend
- [ ] `frontend/src/App.tsx` (conditional routing)
- [ ] `frontend/package.json` (dependencies)

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
- **E2E Tests:** Playwright with Auth0 authentication flow

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
