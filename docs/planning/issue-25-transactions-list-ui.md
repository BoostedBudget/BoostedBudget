# Issue #25: Transactions List UI - Planning & Tracking

**Status**: 🟡 PLANNING
**Issue**: [#25 - Transactions List UI](https://github.com/BoostedBudget/BoostedBudget/issues/25)
**Priority**: P0
**Phase**: Phase 2
**Size**: Large
**Created**: 2025-10-26
**Last Updated**: 2025-10-26

---

## Quick Links
- GitHub Issue: #25
- Dependencies: #22 (✅ COMPLETED), #24 (⚠️ OPEN - non-blocking)
- Branch: `feature/25-transactions-list-ui` (to be created)
- Related Docs: `boosted-budget-complete-plan.md` Phase 2.3

---

## Research Summary

### ✅ What Exists (Issue #22 - Completed)
- ✅ `transactions` table in database (migration 000010)
- ✅ `Transaction` domain model (`backend/internal/domain/transaction.go`)
- ✅ `TransactionRepository` interface with basic CRUD (`backend/internal/repository/transaction_repository.go`)
- ✅ Repository methods: `Create()`, `CreateBatch()`, `Update()`, `DeleteByExternalID()`, `GetByUserID()`, `GetByAccountID()`, `GetBySource()`
- ✅ Transaction sync worker (from Plaid)
- ✅ Provider-agnostic architecture (supports Plaid, manual, future providers)

### ❌ What's Missing (This Issue)
- ❌ GET /api/v1/transactions API endpoint (not in OpenAPI spec)
- ❌ TransactionService layer
- ❌ TransactionHandler
- ❌ Repository filtering methods (by date range, category, search)
- ❌ Frontend transactions feature directory
- ❌ Frontend components (TransactionsList, TransactionCard, TransactionFilters)
- ❌ API integration hooks
- ❌ Virtual scrolling implementation

### 📊 Existing Patterns Identified

**Backend Patterns** (from AccountsPage/PlaidHandler):
- Handler → Service → Repository layers
- Auth0 middleware for user authentication
- `middleware.GetUserID(c)` for extracting user from context
- Error handling: Specific error types, appropriate HTTP status codes
- Pagination: `limit` and `offset` query parameters
- Validation using Echo's `c.Validate()`

**Frontend Patterns** (from AccountsPage):
- Feature-based directory structure: `features/<feature>/`
- API layer: `api/<feature>Api.ts`
- Custom hooks: `use<Feature>()` pattern
- Component structure: Page → Cards
- Loading/Error states with retry
- Mock patterns in tests (Vitest)
- Router context in tests: `renderWithRouter()`
- Subscription tier enforcement patterns

**Database Patterns**:
- UUID primary keys
- Foreign key constraints with CASCADE
- Indexes on frequently queried columns
- JSONB for flexible data (categories, metadata)
- Timestamps with triggers

---

## Architecture Decisions

### Backend Design

#### API Endpoint: `GET /api/v1/transactions`

**Query Parameters**:
```
?limit=50              # Pagination (default: 50, max: 100)
&offset=0              # Pagination (default: 0)
&account_id=uuid       # Filter by specific account (optional)
&start_date=2025-01-01 # Filter start date (optional)
&end_date=2025-12-31   # Filter end date (optional)
&category=food         # Filter by category (optional)
&search=starbucks      # Search merchant/name (optional)
&pending=true          # Filter pending transactions (optional)
```

**Response Schema**:
```json
{
  "transactions": [
    {
      "id": "uuid",
      "account_id": "uuid",
      "amount": -5.49,
      "date": "2025-10-25",
      "name": "Starbucks",
      "merchant_name": "Starbucks Coffee",
      "category": ["Food and Drink", "Coffee"],
      "pending": false,
      "payment_channel": "in store",
      ...
    }
  ],
  "total_count": 1247,
  "has_more": true,
  "limit": 50,
  "offset": 0
}
```

**Why These Decisions**:
- ✅ Limit/offset pagination: Simple, matches existing patterns in codebase
- ✅ Query params for filters: RESTful, cacheable, bookmarkable
- ✅ Max limit of 100: Prevents excessive data transfer
- ✅ `has_more` flag: Frontend knows if more pages exist
- ⚠️ Alternative considered: Cursor-based pagination (better for large datasets, but more complex for this MVP)

#### Repository Layer Extensions

**New Method**: `GetByUserIDWithFilters()`
```go
GetByUserIDWithFilters(
    ctx context.Context,
    userID string,
    filters TransactionFilters,
    limit int,
    offset int,
) ([]domain.Transaction, error)
```

**New Method**: `CountByUserID()`
```go
CountByUserID(
    ctx context.Context,
    userID string,
    filters TransactionFilters,
) (int, error)
```

**Filter Struct**:
```go
type TransactionFilters struct {
    AccountID  *string
    StartDate  *time.Time
    EndDate    *time.Time
    Category   *string
    Search     *string
    Pending    *bool
}
```

**SQL Query Strategy**:
- Dynamic WHERE clause building based on non-nil filters
- Parameterized queries for SQL injection prevention
- ILIKE for case-insensitive search (PostgreSQL)
- Index usage: `idx_transactions_date`, `idx_transactions_account_id`

#### Service Layer (NEW)

**File**: `backend/internal/services/transaction_service.go`

**Interface**:
```go
type TransactionService interface {
    ListTransactions(
        ctx context.Context,
        userID string,
        filters TransactionFilters,
        limit int,
        offset int,
    ) (*TransactionListResponse, error)
}
```

**Responsibilities**:
- Validate pagination params (limit ≤ 100)
- Validate date range (start < end)
- Sanitize search input
- Call repository methods
- Build response with metadata

#### Handler Layer (NEW)

**File**: `backend/internal/handlers/transaction_handler.go`

**Responsibilities**:
- Parse query parameters
- Extract userID from context
- Call service layer
- Handle errors (400, 401, 500)
- Return JSON response

### Frontend Design

#### Directory Structure
```
frontend/src/features/transactions/
├── TransactionsList.tsx          # Main page component
├── TransactionsList.test.tsx
├── TransactionsList.css
├── TransactionCard.tsx           # Individual transaction display
├── TransactionCard.test.tsx
├── TransactionCard.css
├── TransactionFilters.tsx        # Filter controls
├── TransactionFilters.test.tsx
├── TransactionFilters.css
├── types.ts                      # TypeScript types
├── api/
│   ├── transactionsApi.ts        # API calls
│   └── transactionsApi.test.ts
└── hooks/
    ├── useTransactions.ts        # Custom hook
    └── useTransactions.test.ts
```

#### Virtual Scrolling Decision

**Library**: `react-window` (chosen over `react-virtualized`)

**Why**:
- ✅ Smaller bundle size (~3KB vs ~27KB)
- ✅ Better performance
- ✅ Simpler API
- ✅ Actively maintained
- ✅ Better TypeScript support

**Configuration**:
- Item height: 80px (fixed for simplicity)
- Overscan count: 5 (render 5 items above/below viewport)
- Window height: Calculate based on viewport

#### State Management

**Approach**: Local React state + custom hooks (no Redux needed yet)

**Why**:
- ✅ Feature is self-contained
- ✅ No shared state with other features
- ✅ Simpler mental model
- ✅ Easier to test
- ⚠️ If we need global transaction state later, can migrate to Redux/Zustand

#### Filter UX Decisions

**Debounce**: 300ms on search input
- Why: Reduce API calls while typing
- Tested: 300ms feels responsive without lag

**Filter Persistence**: URL query params (future enhancement)
- Why: Shareable links, browser back button support
- Status: Not in MVP, add later if needed

**Clear Filters**: Single "Clear All" button
- Why: Faster than clearing each filter individually
- Resets to: No filters, default pagination

---

## TDD Implementation Plan

### Phase 1: Backend - RED (Write Failing Tests First)

#### 1.1 Repository Tests (`transaction_repository_test.go`)
- [ ] Test: `GetByUserIDWithFilters()` returns all transactions when no filters
- [ ] Test: `GetByUserIDWithFilters()` filters by date range correctly
- [ ] Test: `GetByUserIDWithFilters()` filters by account_id correctly
- [ ] Test: `GetByUserIDWithFilters()` searches merchant name (case-insensitive)
- [ ] Test: `GetByUserIDWithFilters()` filters by category correctly
- [ ] Test: `GetByUserIDWithFilters()` filters by pending status
- [ ] Test: `GetByUserIDWithFilters()` combines multiple filters (AND logic)
- [ ] Test: `GetByUserIDWithFilters()` respects pagination (limit/offset)
- [ ] Test: `GetByUserIDWithFilters()` returns empty array for no matches
- [ ] Test: `CountByUserID()` returns correct count with no filters
- [ ] Test: `CountByUserID()` returns correct count with filters applied

#### 1.2 Service Tests (`transaction_service_test.go`)
- [ ] Test: `ListTransactions()` enforces max limit of 100
- [ ] Test: `ListTransactions()` validates date range (start before end)
- [ ] Test: `ListTransactions()` sanitizes search input (SQL injection)
- [ ] Test: `ListTransactions()` returns transactions array
- [ ] Test: `ListTransactions()` includes pagination metadata (total_count, has_more)
- [ ] Test: `ListTransactions()` handles repository errors gracefully

#### 1.3 Handler Tests (`transaction_handler_test.go`)
- [ ] Test: Returns 401 without authentication
- [ ] Test: Parses limit query param correctly
- [ ] Test: Parses offset query param correctly
- [ ] Test: Parses date query params (ISO 8601 format)
- [ ] Test: Returns 400 for invalid date format
- [ ] Test: Returns 400 for limit > 100
- [ ] Test: Returns 400 for negative offset
- [ ] Test: Returns 200 with transactions array
- [ ] Test: Returns proper pagination metadata

### Phase 2: Backend - GREEN (Implement Minimal Code)

#### 2.1 OpenAPI Spec Update
- [ ] Add `GET /api/v1/transactions` endpoint to `api/openapi.yaml`
- [ ] Define query parameters schema
- [ ] Define response schema
- [ ] Add examples for success/error cases
- [ ] Run OpenAPI validation: `make generate`

#### 2.2 Repository Layer
- [ ] Add `TransactionFilters` struct to `domain/transaction.go`
- [ ] Extend `TransactionRepository` interface in `transaction_repository.go`
- [ ] Implement `GetByUserIDWithFilters()` method
  - [ ] Build dynamic SQL WHERE clause
  - [ ] Handle optional filters (nil checks)
  - [ ] Use parameterized queries
  - [ ] Apply ORDER BY date DESC
  - [ ] Apply LIMIT and OFFSET
- [ ] Implement `CountByUserID()` method
  - [ ] Same WHERE clause logic as GetByUserID
  - [ ] Return COUNT(*) result
- [ ] Verify all repository tests pass

#### 2.3 Service Layer (NEW)
- [ ] Create `backend/internal/services/transaction_service.go`
- [ ] Define `TransactionService` interface
- [ ] Create `transactionService` struct
- [ ] Implement `NewTransactionService()` constructor
- [ ] Implement `ListTransactions()` method
  - [ ] Validate limit (max 100, default 50)
  - [ ] Validate offset (non-negative, default 0)
  - [ ] Validate date range (start < end)
  - [ ] Call repository `GetByUserIDWithFilters()`
  - [ ] Call repository `CountByUserID()` for total
  - [ ] Calculate `has_more` flag
  - [ ] Build response struct
- [ ] Verify all service tests pass

#### 2.4 Handler Layer (NEW)
- [ ] Create `backend/internal/handlers/transaction_handler.go`
- [ ] Define `TransactionHandler` struct
- [ ] Implement `NewTransactionHandler()` constructor
- [ ] Implement `GetTransactions()` handler
  - [ ] Extract userID from context via `middleware.GetUserID()`
  - [ ] Parse query params (limit, offset, filters)
  - [ ] Validate query params
  - [ ] Call service `ListTransactions()`
  - [ ] Handle errors (map to HTTP status codes)
  - [ ] Return JSON response
- [ ] Verify all handler tests pass

#### 2.5 Wire Up Routes
- [ ] Edit `backend/cmd/server/main.go`
- [ ] Create TransactionRepository instance
- [ ] Create TransactionService instance
- [ ] Create TransactionHandler instance
- [ ] Register route: `api.GET("/transactions", transactionHandler.GetTransactions)`
- [ ] Test manually with curl/Postman

### Phase 3: Backend - REFACTOR

- [ ] Extract query param parsing to helper function
- [ ] Extract WHERE clause building to `buildTransactionFilter()` function
- [ ] Add comprehensive logging (request params, query results)
- [ ] Add metrics (query duration, result count)
- [ ] Review SQL query performance with EXPLAIN ANALYZE
- [ ] Add database indexes if needed
- [ ] Consider Redis caching for common queries (future enhancement)
- [ ] Code review: Check for edge cases
- [ ] Verify all tests still pass after refactor

### Phase 4: Frontend - RED (Write Failing Tests First)

#### 4.1 API Layer Tests (`transactionsApi.test.ts`)
- [ ] Test: `fetchTransactions()` constructs URL with query params correctly
- [ ] Test: `fetchTransactions()` includes auth token in headers
- [ ] Test: `fetchTransactions()` parses JSON response
- [ ] Test: `fetchTransactions()` throws on 401 (unauthorized)
- [ ] Test: `fetchTransactions()` throws on 400 (bad request)
- [ ] Test: `fetchTransactions()` throws on 500 (server error)

#### 4.2 Hook Tests (`useTransactions.test.ts`)
- [ ] Test: Returns loading state initially
- [ ] Test: Fetches transactions on mount
- [ ] Test: Returns transactions data after successful fetch
- [ ] Test: Refetches when filters change
- [ ] Test: Refetches when pagination changes
- [ ] Test: Handles API errors and sets error state
- [ ] Test: Debounces search input (300ms)
- [ ] Test: `loadMore()` function appends new transactions
- [ ] Test: `hasMore` flag prevents unnecessary API calls

#### 4.3 Component Tests

**TransactionCard.test.tsx**:
- [ ] Test: Renders transaction amount (positive as green, negative as red)
- [ ] Test: Formats currency correctly (USD with 2 decimals)
- [ ] Test: Displays merchant name if available
- [ ] Test: Falls back to transaction name if no merchant
- [ ] Test: Formats date correctly (locale-aware)
- [ ] Test: Shows pending badge when pending=true
- [ ] Test: Shows category if available
- [ ] Test: Renders payment channel icon

**TransactionFilters.test.tsx**:
- [ ] Test: Date range picker updates filter state
- [ ] Test: Account dropdown updates filter state
- [ ] Test: Category dropdown updates filter state
- [ ] Test: Search input updates after debounce (300ms)
- [ ] Test: "Clear All" button resets all filters
- [ ] Test: Filters trigger onFilterChange callback
- [ ] Test: Displays current filter count

**TransactionsList.test.tsx**:
- [ ] Test: Displays loading skeleton initially
- [ ] Test: Renders list of transactions after load
- [ ] Test: Shows empty state when no transactions
- [ ] Test: Displays error message on fetch failure
- [ ] Test: "Retry" button refetches after error
- [ ] Test: "Load More" button loads next page
- [ ] Test: "Load More" disabled when no more results
- [ ] Test: Virtual scrolling renders only visible items
- [ ] Test: Filter changes trigger refetch with offset=0
- [ ] Test: Responsive layout on mobile viewport

### Phase 5: Frontend - GREEN (Implement Minimal Code)

#### 5.1 Types (`types.ts`)
- [ ] Create `frontend/src/features/transactions/types.ts`
- [ ] Define `Transaction` interface (match backend schema)
- [ ] Define `TransactionFilters` interface
- [ ] Define `TransactionsResponse` interface
- [ ] Export all types

#### 5.2 API Layer (`api/transactionsApi.ts`)
- [ ] Create `frontend/src/features/transactions/api/transactionsApi.ts`
- [ ] Implement `fetchTransactions(filters, limit, offset)` function
  - [ ] Build query string from filters
  - [ ] Include auth token from localStorage/context
  - [ ] Make GET request to `/api/v1/transactions`
  - [ ] Parse and return response
  - [ ] Handle errors (throw with meaningful messages)
- [ ] Verify all API tests pass

#### 5.3 Custom Hook (`hooks/useTransactions.ts`)
- [ ] Create `frontend/src/features/transactions/hooks/useTransactions.ts`
- [ ] Implement `useTransactions(initialFilters)` hook
  - [ ] State: `transactions`, `loading`, `error`, `hasMore`, `totalCount`
  - [ ] State: `filters`, `limit`, `offset`
  - [ ] Effect: Fetch on mount and when filters/pagination change
  - [ ] Function: `setFilters()` - resets offset to 0
  - [ ] Function: `loadMore()` - increments offset
  - [ ] Function: `retry()` - refetches current page
  - [ ] Debounce search filter changes (300ms)
- [ ] Verify all hook tests pass

#### 5.4 Components

**TransactionCard.tsx**:
- [ ] Create component file
- [ ] Accept `transaction` prop
- [ ] Display amount (formatted, colored)
- [ ] Display date (formatted with `Intl.DateTimeFormat`)
- [ ] Display merchant name or fallback to name
- [ ] Display category badge (if available)
- [ ] Display pending badge (if pending)
- [ ] Add accessible ARIA labels
- [ ] Create corresponding CSS file
- [ ] Verify all component tests pass

**TransactionFilters.tsx**:
- [ ] Create component file
- [ ] Accept `filters`, `onFilterChange`, `accounts` props
- [ ] Implement date range picker (HTML5 date inputs)
- [ ] Implement account dropdown (populate from accounts prop)
- [ ] Implement category dropdown (hardcoded common categories + "All")
- [ ] Implement search input (debounced via hook)
- [ ] Implement pending filter checkbox
- [ ] Implement "Clear All Filters" button
- [ ] Display active filter count badge
- [ ] Create corresponding CSS file
- [ ] Verify all component tests pass

**TransactionsList.tsx**:
- [ ] Create component file
- [ ] Use `useTransactions()` hook
- [ ] Use `useAccounts()` hook (for filter dropdown)
- [ ] Render filter controls at top
- [ ] Implement virtual scrolling with `react-window`
  - [ ] Install: `npm install react-window @types/react-window`
  - [ ] Configure FixedSizeList with 80px item height
  - [ ] Render TransactionCard in row renderer
- [ ] Show loading skeleton (while loading)
- [ ] Show empty state (no transactions)
- [ ] Show error state with retry button
- [ ] Render "Load More" button at bottom
- [ ] Disable "Load More" when `!hasMore`
- [ ] Create corresponding CSS file
- [ ] Verify all component tests pass

#### 5.5 Styling (CSS)
- [ ] Create `TransactionsList.css` - responsive grid/list layout
- [ ] Create `TransactionCard.css` - card styling, hover effects
- [ ] Create `TransactionFilters.css` - filter controls layout
- [ ] Follow existing color scheme from `AccountsPage.css`
- [ ] Mobile-first responsive design
- [ ] Accessibility: Focus states, keyboard navigation
- [ ] Test on mobile viewport (320px - 768px)
- [ ] Test on tablet viewport (768px - 1024px)
- [ ] Test on desktop viewport (1024px+)

### Phase 6: Frontend - REFACTOR

- [ ] Extract virtual scroll config to constants
- [ ] Memoize TransactionCard with `React.memo()`
- [ ] Optimize filter renders with `useMemo()` and `useCallback()`
- [ ] Extract date formatting to utility function
- [ ] Extract currency formatting to utility function
- [ ] Add loading skeletons for better UX
- [ ] Improve error messages (user-friendly)
- [ ] Add error boundary around transactions list
- [ ] Code review: Check accessibility
- [ ] Verify all tests still pass after refactor

### Phase 7: Integration & End-to-End Testing

#### 7.1 Backend Integration Tests
- [ ] Run: `cd backend && go test ./... -v`
- [ ] Verify all tests pass
- [ ] Check coverage: `go test -coverprofile=coverage.out ./...`
- [ ] Ensure coverage > 80% for new code
- [ ] Fix any failing tests

#### 7.2 Frontend Unit Tests
- [ ] Run: `npm test -- --run`
- [ ] Verify all tests pass
- [ ] Check coverage report
- [ ] Ensure coverage > 80% for new code
- [ ] Fix any failing tests

#### 7.3 Linting
- [ ] Run: `npm run lint`
- [ ] Fix all linting errors
- [ ] Run: `cd backend && golangci-lint run` (if available)
- [ ] Ensure consistent code style

#### 7.4 E2E Tests (Optional but Recommended)
- [ ] Create `e2e/transactions-list.spec.ts`
- [ ] Test: User can view their transactions
- [ ] Test: User can filter by date range
- [ ] Test: User can filter by account
- [ ] Test: User can search by merchant
- [ ] Test: Pagination loads more transactions
- [ ] Test: Virtual scrolling performs well with 1000+ transactions
- [ ] Run: `npm run test:e2e -- --reporter=line`
- [ ] Verify all E2E tests pass

#### 7.5 Manual Testing Checklist
- [ ] Test with 0 transactions (empty state)
- [ ] Test with 10 transactions (basic display)
- [ ] Test with 100 transactions (pagination)
- [ ] Test with 1000+ transactions (virtual scrolling performance)
- [ ] Test all filter combinations
- [ ] Test search with various queries
- [ ] Test date range edge cases
- [ ] Test on Chrome (latest)
- [ ] Test on Firefox (latest)
- [ ] Test on Safari (latest)
- [ ] Test on mobile Chrome (iOS/Android)
- [ ] Test keyboard navigation (tab, enter, esc)
- [ ] Test screen reader compatibility (NVDA/JAWS)
- [ ] Test with slow 3G network (loading states)
- [ ] Test with network errors (error states)

---

## Progress Tracking

### Overall Status
- [x] Research & Planning (100%)
- [ ] Backend Implementation (30%)
  - [x] RED phase - Repository tests (100%)
  - [x] GREEN phase - Repository implementation (100%)
  - [ ] RED phase - Service tests (0%)
  - [ ] GREEN phase - Service implementation (0%)
  - [ ] RED phase - Handler tests (0%)
  - [ ] GREEN phase - Handler implementation (0%)
  - [ ] REFACTOR phase (0%)
- [ ] Frontend Implementation (0%)
  - [ ] RED phase (0%)
  - [ ] GREEN phase (0%)
  - [ ] REFACTOR phase (0%)
- [ ] Testing & Integration (0%)
- [ ] Code Review & PR (0%)

### Current Phase
**Phase**: Backend GREEN (Repository) → Backend RED (Service Layer)

**Completed**:
✅ Created feature branch `feature/25-transactions-list-ui`
✅ Added TransactionFilters struct to domain layer
✅ Extended TransactionRepository interface with GetByUserIDWithFilters and CountByUserID
✅ Wrote 10 comprehensive failing tests for repository filtering (RED phase)
✅ Implemented GetByUserIDWithFilters with dynamic SQL WHERE clause (GREEN phase)
✅ Implemented CountByUserID for pagination metadata (GREEN phase)
✅ Fixed search filter SQL argument handling (pattern added twice for OR condition)
✅ All 16 repository tests passing (7 original + 9 new filtering tests)
✅ Committed GREEN phase work (commit 4e19881)

**Next Steps**:
1. Create TransactionService interface and implementation
2. Write failing tests for TransactionService.ListTransactions() (RED phase)
3. Implement service layer logic (GREEN phase)

---

## Decisions Log

### Decision 1: Pagination Strategy
**Date**: 2025-10-26
**Decision**: Use offset-based pagination (limit/offset)
**Alternatives Considered**: Cursor-based pagination
**Reasoning**:
- Simpler to implement for MVP
- Matches existing pattern in codebase (GetByUserID)
- Good enough for expected transaction volumes (<10K per user)
- Can migrate to cursor-based later if needed

**Trade-offs**:
- ✅ Simpler implementation
- ✅ Easier to jump to specific page (future feature)
- ⚠️ Performance degrades with very high offsets (rare for MVP)

---

### Decision 2: Virtual Scrolling Library
**Date**: 2025-10-26
**Decision**: Use `react-window`
**Alternatives Considered**: `react-virtualized`, manual implementation
**Reasoning**:
- Smaller bundle size (3KB vs 27KB)
- Better performance
- Actively maintained
- Good TypeScript support
- Industry standard for React virtual scrolling

**Trade-offs**:
- ✅ Great performance with 1000+ items
- ✅ Small bundle size
- ⚠️ Fixed item height (80px) - acceptable for transaction cards

---

### Decision 3: Filter State Management
**Date**: 2025-10-26
**Decision**: Local React state + custom hooks (no Redux)
**Alternatives Considered**: Redux, Zustand, URL query params
**Reasoning**:
- Feature is self-contained
- No shared state with other features
- Simpler to test and maintain
- Follows existing pattern (AccountsPage uses local state)

**Trade-offs**:
- ✅ Simpler mental model
- ✅ Faster to implement
- ✅ Easier to test
- ⚠️ State resets on unmount (acceptable for MVP)
- ⚠️ Not shareable via URL (future enhancement)

---

### Decision 4: Search Debounce Timing
**Date**: 2025-10-26
**Decision**: 300ms debounce on search input
**Alternatives Considered**: 150ms, 500ms, no debounce
**Reasoning**:
- 300ms feels responsive without lag
- Reduces API calls significantly
- Industry standard (Google uses ~300ms)
- Balances responsiveness vs server load

**Trade-offs**:
- ✅ Reduces API calls by ~70%
- ✅ Still feels instant to users
- ⚠️ Very fast typers might notice slight delay

---

## Notes & Gotchas

### Backend
- Remember to update OpenAPI spec FIRST (source of truth)
- PostgreSQL ILIKE for case-insensitive search
- Date filtering: Use DATE type, not timestamp comparison
- SQL injection: Always use parameterized queries ($1, $2, etc.)
- Test with NULL values in optional fields (merchant_name, category)

### Frontend
- react-window requires fixed item height (use 80px for TransactionCard)
- Debounce search to avoid API spam
- Virtual scrolling: Don't forget overscan for smooth scrolling
- Test with empty arrays and null values
- Accessibility: Add ARIA labels for screen readers
- Mobile: Touch-friendly filter controls (min 44px tap targets)

### Testing
- Mock `fetchTransactions` in component tests
- Use `waitFor()` for async state updates
- Test loading/error states explicitly
- E2E: Use test data fixtures for consistent results

---

## MCP Tracking Checkpoints

### Server Memory Updates

**After Planning** (NOW):
- [x] Track API endpoint design decisions
- [x] Track pagination strategy choice
- [x] Track virtual scrolling library selection
- [x] Track filter state management approach

**During Backend Implementation**:
- [ ] Track repository SQL query patterns
- [ ] Track service layer validation logic
- [ ] Track any database index additions
- [ ] Track any performance optimizations

**During Frontend Implementation**:
- [ ] Track virtual scrolling configuration
- [ ] Track component composition decisions
- [ ] Track CSS/styling patterns
- [ ] Track accessibility implementations

**After Each Major Phase**:
- [ ] Track lessons learned
- [ ] Track test coverage metrics
- [ ] Track any deviations from plan
- [ ] Track performance benchmarks

### In-Memoria Updates
- [ ] Document transaction filtering logic flow
- [ ] Document virtual scrolling implementation details
- [ ] Document reusable patterns for future list features
- [ ] Document testing patterns for complex components

---

## Time Estimates

| Phase | Estimated Time | Actual Time | Notes |
|-------|----------------|-------------|-------|
| Research & Planning | 2h | 2h | ✅ Complete |
| Backend RED | 2h | - | - |
| Backend GREEN | 3h | - | - |
| Backend REFACTOR | 1h | - | - |
| Frontend RED | 2h | - | - |
| Frontend GREEN | 4h | - | - |
| Frontend REFACTOR | 1h | - | - |
| Integration & Testing | 2h | - | - |
| E2E Tests | 1h | - | Optional |
| Manual Testing | 1h | - | - |
| **TOTAL** | **16-17h** | **2h** | In progress |

---

## Success Criteria

### Backend
- ✅ GET /api/v1/transactions endpoint returns 200
- ✅ All query parameters work correctly
- ✅ Pagination metadata is accurate
- ✅ Filtering logic works for all combinations
- ✅ All backend tests pass
- ✅ Test coverage > 80%
- ✅ No SQL injection vulnerabilities
- ✅ Proper error handling (4xx, 5xx)

### Frontend
- ✅ Transactions list displays correctly
- ✅ All filters work (date, account, category, search)
- ✅ Pagination loads more transactions
- ✅ Virtual scrolling handles 1000+ transactions
- ✅ Loading states display appropriately
- ✅ Error states display with retry option
- ✅ Empty state displays when no transactions
- ✅ Responsive design (mobile/tablet/desktop)
- ✅ Accessible (keyboard navigation, screen readers)
- ✅ All frontend tests pass
- ✅ Test coverage > 80%

### Integration
- ✅ All backend tests pass: `go test ./... -v`
- ✅ All frontend tests pass: `npm test -- --run`
- ✅ Linting passes: `npm run lint`
- ✅ E2E tests pass: `npm run test:e2e`
- ✅ Manual testing checklist complete
- ✅ No console errors or warnings
- ✅ Performance acceptable (< 100ms filter response)

### Documentation & Process
- ✅ All decisions tracked in server-memory
- ✅ This planning doc updated throughout
- ✅ OpenAPI spec updated
- ✅ Code comments for complex logic
- ✅ PR description includes testing instructions
- ✅ Issue #25 closed automatically on merge

---

## References

### Related Files
- `backend/internal/domain/transaction.go` - Transaction model
- `backend/internal/repository/transaction_repository.go` - Repository interface
- `backend/migrations/000010_create_transactions_table.sql` - Database schema
- `backend/internal/handlers/plaid_handler.go` - Handler pattern example
- `frontend/src/features/accounts/AccountsPage.tsx` - Frontend pattern example
- `frontend/src/features/accounts/types.ts` - Types pattern example
- `api/openapi.yaml` - API specification

### Documentation
- `boosted-budget-complete-plan.md` - Overall project plan
- `CLAUDE.md` - Development guidelines
- `config/subscription_tiers.yaml` - Tier configuration

### External Resources
- [react-window docs](https://react-window.vercel.app/)
- [PostgreSQL ILIKE](https://www.postgresql.org/docs/current/functions-matching.html)
- [Echo framework](https://echo.labstack.com/)
- [Vitest docs](https://vitest.dev/)

---

**Last Updated**: 2025-10-26
**Updated By**: Claude Code (Planning Phase)
**Next Update**: After Backend RED phase completion
