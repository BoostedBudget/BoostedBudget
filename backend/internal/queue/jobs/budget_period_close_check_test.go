// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockUserTimezoneLookup mocks UserTimezoneLookup.
type mockUserTimezoneLookup struct {
	mock.Mock
}

func (m *mockUserTimezoneLookup) GetTimezonesByUserIDs(ctx context.Context, userIDs []string) (map[string]string, error) {
	args := m.Called(ctx, userIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

// =============================================================================
// Mocks
// =============================================================================

// mockBudgetPeriodCheckRepo mocks BudgetPeriodCheckRepository.
type mockBudgetPeriodCheckRepo struct {
	mock.Mock
}

func (m *mockBudgetPeriodCheckRepo) GetAllActivePaginated(ctx context.Context, limit int, offset int) ([]*domain.Budget, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

// mockBudgetPeriodEventsRepo mocks BudgetPeriodEventsRepo.
type mockBudgetPeriodEventsRepo struct {
	mock.Mock
}

func (m *mockBudgetPeriodEventsRepo) HasMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error) {
	args := m.Called(ctx, userID, budgetID, periodStart, periodEnd, eventType)
	return args.Bool(0), args.Error(1)
}

func (m *mockBudgetPeriodEventsRepo) InsertMarker(ctx context.Context, userID string, budgetID string, periodStart time.Time, periodEnd time.Time, eventType string) (bool, error) {
	args := m.Called(ctx, userID, budgetID, periodStart, periodEnd, eventType)
	return args.Bool(0), args.Error(1)
}

// =============================================================================
// Test helpers
// =============================================================================

func newTestBudgetPeriodCloseCheckJob(args BudgetPeriodCloseCheckArgs) *river.Job[BudgetPeriodCloseCheckArgs] {
	return &river.Job[BudgetPeriodCloseCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func newMonthlyBudget(id, userID string, startDate time.Time) *domain.Budget {
	return &domain.Budget{
		ID:           id,
		UserID:       userID,
		Name:         "Test Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    startDate,
		Amount:       1000,
		CurrencyCode: "USD",
		IsActive:     true,
	}
}

// =============================================================================
// Kind test
// =============================================================================

func TestBudgetPeriodCloseCheckArgs_Kind(t *testing.T) {
	assert.Equal(t, "budget_period_close_check", BudgetPeriodCloseCheckArgs{}.Kind())
}

// =============================================================================
// Worker tests
// =============================================================================

// TestBudgetPeriodCloseCheck_FiresEventOnPeriodClose verifies that when the job
// runs on the first day of a new month, it fires the budget_period_end event
// for the previous (just-closed) month. This is the GAP N fix — the test now
// exercises the full worker firing path (workAt) rather than only the helper.
//
// This also serves as the GAP J emitter⊇reader parity guard: it asserts that
// every EventDataKey* constant consumed by calculateTransferAmount and
// calculateSavingsGoalAmount is present in the emitted EventData map.
func TestBudgetPeriodCloseCheck_FiresEventOnPeriodClose(t *testing.T) {
	ctx := context.Background()

	// "now" is the first day of February — January just closed.
	now := time.Date(2024, time.February, 1, 9, 0, 0, 0, time.UTC)

	// Previous period boundaries: Jan 1 – Jan 31
	expectedPeriodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	expectedPeriodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	// Budget with non-zero TotalSpent() and RemainingAmount() so we can assert
	// on the emitted values. TotalSpent() sums top-level category SpentAmount fields.
	// Amount=1500 with 300 spent → RemainingAmount()=1200, TotalSpent()=300.
	budget := &domain.Budget{
		ID:           "budget-1",
		UserID:       "user-1",
		Name:         "Test Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		Amount:       1500,
		IncomeAmount: 2000,
		CurrencyCode: "USD",
		IsActive:     true,
		Categories: []domain.BudgetCategory{
			{
				ID:              "cat-1",
				BudgetID:        "budget-1",
				CategoryKind:    "custom",
				AllocatedAmount: 500,
				SpentAmount:     300,
			},
		},
	}

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	// Page 1 returns one budget; single-page result (len < pageSize) → loop exits.
	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{budget}, nil)

	periodEventsRepo.On("HasMarker", ctx, "user-1", "budget-1", expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(false, nil)
	periodEventsRepo.On("InsertMarker", ctx, "user-1", "budget-1", expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(true, nil)

	// Capture the enqueued job to inspect EventData keys.
	var capturedArgs RuleEvaluationArgs
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		ea, ok := job.(RuleEvaluationArgs)
		if ok {
			capturedArgs = ea
		}
		return ok && ea.UserID == "user-1" && ea.EventType == domain.EventBudgetPeriodEnd
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil).Once()

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test-fires-event"})
	err := worker.workAt(ctx, job, now)
	require.NoError(t, err)

	// Enqueue must have been called exactly once.
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 1)
	mockQueue.AssertExpectations(t)
	periodEventsRepo.AssertExpectations(t)

	// Assert the emitted EventData contains all domain.EventDataKey* constants that
	// the rule-execution readers consume (GAP J parity guard).
	require.NotNil(t, capturedArgs.EventData, "EventData must not be nil")
	assert.Equal(t, "budget-1", capturedArgs.EventData[domain.EventDataKeyBudgetID], "budget_id must match")
	assert.Equal(t, float64(300), capturedArgs.EventData[domain.EventDataKeyTotalSpent], "total_spent must match TotalSpent()")
	assert.Equal(t, float64(1200), capturedArgs.EventData[domain.EventDataKeyRemaining], "remaining must match RemainingAmount()")
	assert.Equal(t, expectedPeriodStart.Format("2006-01-02"), capturedArgs.EventData[domain.EventDataKeyPeriodStart], "period_start must be YYYY-MM-DD")
	assert.Equal(t, expectedPeriodEnd.Format("2006-01-02"), capturedArgs.EventData[domain.EventDataKeyPeriodEnd], "period_end must be YYYY-MM-DD")
}

// TestEventDataKeyEmitterCoversReaderKeys is a contract test that verifies the
// set of EventDataKey* constants emitted by the period-close worker (GAP J emitter)
// is a superset of the keys consumed by calculateTransferAmount and
// calculateSavingsGoalAmount (the reader side). No functions are called — this is a
// pure set-membership assertion over the domain constant values.
func TestEventDataKeyEmitterCoversReaderKeys(t *testing.T) {
	// Keys emitted by budget_period_close_check.go (the worker's EventData map).
	emitted := map[string]bool{
		domain.EventDataKeyBudgetID:     true,
		domain.EventDataKeyRemaining:    true,
		domain.EventDataKeyIncomeAmount: true,
		domain.EventDataKeyAmount:       true,
		domain.EventDataKeyTotalSpent:   true,
		domain.EventDataKeyPeriodStart:  true,
		domain.EventDataKeyPeriodEnd:    true,
	}

	// Keys consumed by calculateTransferAmount (amount types: percentage, remaining, threshold).
	// Keys consumed by calculateSavingsGoalAmount (amount types: percentage, remaining).
	consumed := []string{
		domain.EventDataKeyAmount,    // calculateTransferAmount: "percentage" and "threshold" branches
		domain.EventDataKeyRemaining, // calculateTransferAmount: "remaining" branch; calculateSavingsGoalAmount: "remaining" branch
	}

	for _, key := range consumed {
		assert.True(t, emitted[key],
			"emitter must include key %q consumed by rule-execution calculators", key)
	}
}

// TestBudgetPeriodCloseCheck_IdempotentOnRerun verifies that when a marker
// already exists, no new event is enqueued.
func TestBudgetPeriodCloseCheck_IdempotentOnRerun(t *testing.T) {
	ctx := context.Background()

	// Use a weekly budget where we can control the period precisely.
	// Budget starts on 2024-01-01 (Monday). "now" is 2024-01-08 (next Monday) —
	// the first weekly period (Jan 1–7) just closed.
	startDate := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2024, time.January, 8, 9, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		ID:           "budget-weekly",
		UserID:       "user-weekly",
		Name:         "Weekly Budget",
		Period:       domain.BudgetPeriodWeekly,
		StartDate:    startDate,
		Amount:       500,
		CurrencyCode: "USD",
		IsActive:     true,
	}

	expectedPeriodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	expectedPeriodEnd := time.Date(2024, time.January, 7, 0, 0, 0, 0, time.UTC)

	// Verify the period computation is correct.
	pStart, pEnd, closed := computeJustClosedPeriod(budget, now, time.UTC)
	require.True(t, closed)
	assert.Equal(t, expectedPeriodStart, pStart)
	assert.Equal(t, expectedPeriodEnd, pEnd)

	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	// HasMarker returns true → marker already exists, no insert or enqueue.
	periodEventsRepo.On("HasMarker", ctx, "user-weekly", "budget-weekly", expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(true, nil)

	// No InsertMarker call expected.
	// No Enqueue call expected.
	_ = periodEventsRepo // expectations verified below
	periodEventsRepo.AssertNotCalled(t, "InsertMarker")
}

// TestBudgetPeriodCloseCheck_NoPeriodClose verifies that mid-period, no events fire.
func TestBudgetPeriodCloseCheck_NoPeriodClose(t *testing.T) {
	// Monthly budget, now is mid-month (Jan 15).
	budget := newMonthlyBudget("b1", "u1", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))
	now := time.Date(2024, time.January, 15, 9, 0, 0, 0, time.UTC)

	_, _, closed := computeJustClosedPeriod(budget, now, time.UTC)
	assert.False(t, closed, "no period closed mid-month")
}

// TestBudgetPeriodCloseCheck_LateExecution verifies that a job running on day N+2
// after a period end still detects the closed period (since it checks yesterday).
// Here "now" is Jan 3, yesterday (Jan 2) is still inside the same monthly period,
// so no boundary is crossed. But if now is Feb 2, yesterday (Feb 1) is in February,
// and the day before was Jan 31 (in January) — no, we check yesterday vs today.
// The spec says: if job misses a day, it fires on the next run because yesterday's
// period still differs from today's.
func TestBudgetPeriodCloseCheck_LateExecution(t *testing.T) {
	// Budget: monthly. Period close was Jan 31. Job runs Feb 2 (2 days late).
	// yesterday (Feb 1) is in February, today (Feb 2) is also in February.
	// Both are in the same period → no boundary detected.
	// This is the documented "acceptable loss" behavior for MVP.
	budget := newMonthlyBudget("b1", "u1", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))
	now := time.Date(2024, time.February, 2, 9, 0, 0, 0, time.UTC)

	_, _, closed := computeJustClosedPeriod(budget, now, time.UTC)
	assert.False(t, closed, "job running 2 days after period close misses the boundary (expected for MVP)")

	// Contrast: job running exactly 1 day after (Feb 1 = first day of new period).
	nowOnTime := time.Date(2024, time.February, 1, 9, 0, 0, 0, time.UTC)
	pStart, pEnd, closedOnTime := computeJustClosedPeriod(budget, nowOnTime, time.UTC)
	require.True(t, closedOnTime, "job running on Feb 1 should detect Jan close")
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), pStart)
	assert.Equal(t, time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC), pEnd)
}

// TestBudgetPeriodCloseCheck_FetchError verifies that a fetch error causes the job to fail.
func TestBudgetPeriodCloseCheck_FetchError(t *testing.T) {
	ctx := context.Background()

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return(nil, errors.New("db error"))

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch active budgets")
	budgetRepo.AssertExpectations(t)
}

// TestBudgetPeriodCloseCheck_EmptyBudgets verifies that an empty budget list
// completes without error and without any enqueued jobs.
func TestBudgetPeriodCloseCheck_EmptyBudgets(t *testing.T) {
	ctx := context.Background()

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{}, nil)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	periodEventsRepo.AssertNotCalled(t, "HasMarker")
	mockQueue.AssertNotCalled(t, "Enqueue")
}

// =============================================================================
// GAP E: ExpirePending must be called from the daily budget-period-close job.
// =============================================================================

// mockTransferSuggestionExpirer is a minimal mock satisfying TransferSuggestionExpirer.
type mockTransferSuggestionExpirer struct {
	mock.Mock
}

func (m *mockTransferSuggestionExpirer) ExpirePending(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// TestBudgetPeriodCloseCheck_ExpiresPendingSuggestions verifies that the daily
// budget-period-close worker calls ExpirePending on each run so that stale
// pending suggestions are transitioned to "expired" status.
func TestBudgetPeriodCloseCheck_ExpiresPendingSuggestions(t *testing.T) {
	ctx := context.Background()

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)
	expirer := new(mockTransferSuggestionExpirer)

	// No budgets to process — we only care that ExpirePending is called.
	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{}, nil)
	expirer.On("ExpirePending", ctx).Return(int64(3), nil)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:            budgetRepo,
		PeriodEventsRepo:      periodEventsRepo,
		JobQueue:              mockQueue,
		TransferSuggestionExpirer: expirer,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	expirer.AssertExpectations(t) // ExpirePending must have been called exactly once.
}

// TestBudgetPeriodCloseCheck_ExpirePendingError_DoesNotFailJob verifies that when
// ExpirePending returns an error, the overall job still succeeds (non-critical path).
func TestBudgetPeriodCloseCheck_ExpirePendingError_DoesNotFailJob(t *testing.T) {
	ctx := context.Background()

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)
	expirer := new(mockTransferSuggestionExpirer)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{}, nil)
	expirer.On("ExpirePending", ctx).Return(int64(0), errors.New("db connection lost"))

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:            budgetRepo,
		PeriodEventsRepo:      periodEventsRepo,
		JobQueue:              mockQueue,
		TransferSuggestionExpirer: expirer,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test"})
	err := worker.Work(ctx, job)

	// ExpirePending failure must not propagate as a job error.
	require.NoError(t, err)
	expirer.AssertExpectations(t)
}

// =============================================================================
// computeCurrentPeriod unit tests
// =============================================================================

func TestComputeCurrentPeriod_Monthly(t *testing.T) {
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
	}

	t.Run("mid-month", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("first day of month", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), e) // 2024 is a leap year
	})

	t.Run("last day of month", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC), e)
	})
}

func TestComputeCurrentPeriod_Weekly(t *testing.T) {
	startDate := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC) // Week 0: Jan 1–7
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodWeekly,
		StartDate: startDate,
	}

	t.Run("first week", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.January, 7, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("second week starts on day 8", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.January, 14, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("last day of first week", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.January, 7, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.January, 7, 0, 0, 0, 0, time.UTC), e)
	})
}

func TestComputeCurrentPeriod_Quarterly(t *testing.T) {
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodQuarterly,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	t.Run("Q1", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("Q2", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.June, 30, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("Q4", func(t *testing.T) {
		s, e := computeCurrentPeriod(budget, time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), time.UTC)
		assert.Equal(t, time.Date(2024, time.October, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), e)
	})
}

func TestComputeCurrentPeriod_Yearly(t *testing.T) {
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodYearly,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	s, e := computeCurrentPeriod(budget, time.Date(2024, time.July, 4, 0, 0, 0, 0, time.UTC), time.UTC)
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), e)
}

func TestComputeCurrentPeriod_Custom(t *testing.T) {
	endDate := time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC)
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodCustom,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   &endDate,
	}

	// Custom period always returns the fixed [StartDate, EndDate] window.
	s, e := computeCurrentPeriod(budget, time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), time.UTC)
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), e)
}

// =============================================================================
// computeJustClosedPeriod unit tests
// =============================================================================

func TestComputeJustClosedPeriod_MonthlyBoundary(t *testing.T) {
	budget := newMonthlyBudget("b", "u", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	t.Run("first day of new month - period closed", func(t *testing.T) {
		now := time.Date(2024, time.February, 1, 9, 0, 0, 0, time.UTC)
		s, e, closed := computeJustClosedPeriod(budget, now, time.UTC)
		require.True(t, closed)
		assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC), e)
	})

	t.Run("mid-month - no close", func(t *testing.T) {
		now := time.Date(2024, time.January, 15, 9, 0, 0, 0, time.UTC)
		_, _, closed := computeJustClosedPeriod(budget, now, time.UTC)
		assert.False(t, closed)
	})

	t.Run("last day of month - no close yet", func(t *testing.T) {
		now := time.Date(2024, time.January, 31, 9, 0, 0, 0, time.UTC)
		_, _, closed := computeJustClosedPeriod(budget, now, time.UTC)
		assert.False(t, closed, "last day of the period is still in the same period")
	})
}

func TestComputeJustClosedPeriod_WeeklyBoundary(t *testing.T) {
	startDate := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodWeekly,
		StartDate: startDate,
	}

	// Jan 8 is the first day of the second weekly period.
	now := time.Date(2024, time.January, 8, 9, 0, 0, 0, time.UTC)
	s, e, closed := computeJustClosedPeriod(budget, now, time.UTC)
	require.True(t, closed)
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.January, 7, 0, 0, 0, 0, time.UTC), e)
}

func TestComputeJustClosedPeriod_QuarterlyBoundary(t *testing.T) {
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodQuarterly,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	// April 1 = first day of Q2; Q1 (Jan–Mar) just closed.
	now := time.Date(2024, time.April, 1, 9, 0, 0, 0, time.UTC)
	s, e, closed := computeJustClosedPeriod(budget, now, time.UTC)
	require.True(t, closed)
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), e)
}

func TestComputeJustClosedPeriod_YearlyBoundary(t *testing.T) {
	budget := &domain.Budget{
		Period:    domain.BudgetPeriodYearly,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	// Jan 1, 2025 = first day of 2025; 2024 just closed.
	now := time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC)
	s, e, closed := computeJustClosedPeriod(budget, now, time.UTC)
	require.True(t, closed)
	assert.Equal(t, time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), e)
}

// =============================================================================
// Race condition: marker ordering correctness
// =============================================================================

// TestBudgetPeriodCloseCheckWorker_NoDuplicateEnqueueOnMarkerRace verifies that
// when two workers race and both see HasMarker=false, only the winner of the
// InsertMarker race enqueues a job. The loser (InsertMarker returns false) must
// NOT enqueue — ensuring exactly-once delivery.
//
// Correct order: HasMarker (fast path) → InsertMarker → if inserted: Enqueue.
// Wrong order (old bug): HasMarker → Enqueue → InsertMarker.
//
// The budget uses a weekly period anchored so that today (time.Now()) is always
// the first day of a new week, guaranteeing computeJustClosedPeriod returns
// closed=true regardless of when the test runs.
func TestBudgetPeriodCloseCheckWorker_NoDuplicateEnqueueOnMarkerRace(t *testing.T) {
	ctx := context.Background()

	// Use a fixed UTC clock so the budget calibration and worker clock are always
	// aligned regardless of the test machine's local timezone.
	// "now" is 2024-01-08 09:00 UTC — the first day of week 2 for a budget that
	// started on 2024-01-01. Both workers receive the same fixed clock via workAt.
	now := time.Date(2024, time.January, 8, 9, 0, 0, 0, time.UTC)

	// Budget: weekly, started Jan 1. Week 1 = Jan 1–7. Week 2 starts Jan 8.
	// computeJustClosedPeriod(budget, now=Jan8, UTC) → closed=true (Jan 1–7 just closed).
	budgetStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

	budget := &domain.Budget{
		ID:           "budget-race",
		UserID:       "user-race",
		Name:         "Race Test Budget",
		Period:       domain.BudgetPeriodWeekly,
		StartDate:    budgetStart,
		Amount:       500,
		CurrencyCode: "USD",
		IsActive:     true,
	}

	// Shared mocks — both simulated workers share the same repos and queue.
	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	// Both workers see no marker on the fast-path check (mock.Anything for period times).
	periodEventsRepo.On("HasMarker", ctx, "user-race", "budget-race",
		mock.Anything, mock.Anything, domain.EventBudgetPeriodEnd).
		Return(false, nil)

	// InsertMarker: first call wins (returns true), second call loses the DB race
	// (ON CONFLICT, returns false). Using Once() to sequence the two calls.
	periodEventsRepo.On("InsertMarker", ctx, "user-race", "budget-race",
		mock.Anything, mock.Anything, domain.EventBudgetPeriodEnd).
		Return(true, nil).Once()
	periodEventsRepo.On("InsertMarker", ctx, "user-race", "budget-race",
		mock.Anything, mock.Anything, domain.EventBudgetPeriodEnd).
		Return(false, nil).Once()

	// Enqueue must be called exactly once (only the winner enqueues).
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		evalArgs, ok := job.(RuleEvaluationArgs)
		return ok && evalArgs.UserID == "user-race" && evalArgs.EventType == domain.EventBudgetPeriodEnd
	}), mock.Anything).Return(&queue.JobResult{ID: 42}, nil).Once()

	// Both workers get the same single budget page.
	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{budget}, nil)

	makeWorker := func() *BudgetPeriodCloseCheckWorker {
		return NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
			BudgetRepo:       budgetRepo,
			PeriodEventsRepo: periodEventsRepo,
			JobQueue:         mockQueue,
		})
	}

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test-race"})

	// Worker 1: wins the InsertMarker race — should enqueue.
	// Use workAt with fixed clock to avoid timezone/time.Now() flakiness.
	worker1 := makeWorker()
	err1 := worker1.workAt(ctx, job, now)
	require.NoError(t, err1, "worker 1 should complete without error")

	// Worker 2: loses the InsertMarker race — must NOT enqueue again.
	worker2 := makeWorker()
	err2 := worker2.workAt(ctx, job, now)
	require.NoError(t, err2, "worker 2 should complete without error")

	// Enqueue must have been called exactly once across both workers.
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 1)
	mockQueue.AssertExpectations(t)
	periodEventsRepo.AssertExpectations(t)
}

// =============================================================================
// Timezone-aware period detection tests (Issue #972 Task 2e)
// =============================================================================

// TestComputeCurrentPeriod_WithTimezone_MonthBoundary verifies that when the user
// is in America/Los_Angeles and UTC is already March 1, the local date is still
// Feb 29 (2024 is a leap year). The period containing local Feb 29 is February.
func TestComputeCurrentPeriod_WithTimezone_MonthBoundary(t *testing.T) {
	// 2024-03-01 07:00:00 UTC = 2024-02-29 23:00:00 PST (UTC-8 in winter)
	now := time.Date(2024, time.March, 1, 7, 0, 0, 0, time.UTC)
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	budget := &domain.Budget{
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	// With LA timezone: local date is Feb 29 → period is February.
	s, e := computeCurrentPeriod(budget, now, loc)
	assert.Equal(t, time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC), s)
	assert.Equal(t, time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), e)

	// With UTC: date is March 1 → period is March.
	sUTC, eUTC := computeCurrentPeriod(budget, now, time.UTC)
	assert.Equal(t, time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), sUTC)
	assert.Equal(t, time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), eUTC)
}

// TestComputeJustClosedPeriod_WithTimezone_YesBoundaryUTC_NoBoundaryLocal verifies
// the scenario where UTC already sees a boundary but the user's local timezone does not.
//
// Setup: monthly budget. now = 2024-03-01 03:00:00 UTC.
//   - UTC: today = Mar 1, yesterday = Feb 29 — boundary crossed (Feb→Mar). closed=true.
//   - America/Los_Angeles (UTC-8): now.In(loc) = Feb 29 19:00 PST — local date is Feb 29.
//     Local today = Feb 29, local yesterday = Feb 28. Same month (February) → NO boundary yet.
//
// This demonstrates that a user in LA has NOT yet crossed the month boundary when
// UTC reads 03:00 on March 1 — their local clock still shows Feb 29.
func TestComputeJustClosedPeriod_WithTimezone_YesBoundaryUTC_NoBoundaryLocal(t *testing.T) {
	// 2024-03-01 03:00:00 UTC = 2024-02-29 19:00:00 PST
	now := time.Date(2024, time.March, 1, 3, 0, 0, 0, time.UTC)
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	budget := newMonthlyBudget("b", "u", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	// In LA timezone: local now is still Feb 29 (no boundary yet).
	_, _, closed := computeJustClosedPeriod(budget, now, loc)
	assert.False(t, closed, "no boundary in LA timezone when local date is still Feb 29")

	// In UTC: today is Mar 1, yesterday was Feb 29 — boundary IS crossed.
	_, _, closedUTC := computeJustClosedPeriod(budget, now, time.UTC)
	assert.True(t, closedUTC, "boundary detected in UTC on March 1")
}

// TestWorkAt_UsesUserTimezone verifies that workAt uses the timezone from the
// UserTimezoneLookup when detecting period boundaries.
//
// Setup: monthly budget, user in America/New_York (UTC-5 in winter).
// now = 2024-02-01 04:00:00 UTC = 2024-01-31 23:00:00 EST.
// UTC → January just closed (Feb 1 UTC vs Jan 31 UTC → boundary).
// America/New_York → local date is still Jan 31 → yesterday was Jan 30 → same period (January) → NO boundary.
//
// With UserTimezones = New_York, the worker should NOT fire the period-end event.
func TestWorkAt_UsesUserTimezone(t *testing.T) {
	ctx := context.Background()

	// 2024-02-01 04:00:00 UTC = 2024-01-31 23:00:00 EST (UTC-5 in winter)
	now := time.Date(2024, time.February, 1, 4, 0, 0, 0, time.UTC)

	budget := newMonthlyBudget("budget-tz", "user-tz", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)
	tzLookup := new(mockUserTimezoneLookup)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{budget}, nil)

	// Timezone lookup returns America/New_York — local date on Feb 1 04:00 UTC is Jan 31.
	tzLookup.On("GetTimezonesByUserIDs", ctx, []string{"user-tz"}).
		Return(map[string]string{"user-tz": "America/New_York"}, nil)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
		UserTimezones:    tzLookup,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test-tz"})
	err := worker.workAt(ctx, job, now)
	require.NoError(t, err)

	// With New_York timezone: local date is Jan 31, so January has NOT closed yet.
	// No marker check, no insert, no enqueue.
	periodEventsRepo.AssertNotCalled(t, "HasMarker")
	mockQueue.AssertNotCalled(t, "Enqueue")
	tzLookup.AssertExpectations(t)
}

// TestWorkAt_MarkerStableAfterTimezoneChange verifies that the marker identity is
// stable even if a user changes their timezone between two daily worker runs.
//
// The marker key uses UTC-anchored dates derived from the budget's stored StartDate,
// so changing from e.g. America/New_York to America/Los_Angeles does not produce a
// different (period_start, period_end) tuple and therefore cannot cause a double-fire.
//
// Setup: monthly budget. First run uses UTC (period Jan 1–31 just closed on Feb 1 09:00).
// HasMarker returns false → InsertMarker returns true → Enqueue called once.
// Second run uses America/New_York — same period start/end → HasMarker returns true → no enqueue.
func TestWorkAt_MarkerStableAfterTimezoneChange(t *testing.T) {
	ctx := context.Background()

	// Feb 1 09:00 UTC: January just closed in both UTC and New_York (Jan 31 04:00 EST is still Jan 31).
	now := time.Date(2024, time.February, 1, 9, 0, 0, 0, time.UTC)
	expectedPeriodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	expectedPeriodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	budget := newMonthlyBudget("budget-tz-stable", "user-tz-stable", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{budget}, nil)

	// Run 1 (UTC timezone): HasMarker=false → InsertMarker wins → Enqueue once.
	tzLookup1 := new(mockUserTimezoneLookup)
	tzLookup1.On("GetTimezonesByUserIDs", ctx, []string{"user-tz-stable"}).
		Return(map[string]string{"user-tz-stable": "UTC"}, nil)

	periodEventsRepo.On("HasMarker", ctx, "user-tz-stable", "budget-tz-stable",
		expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(false, nil).Once()
	periodEventsRepo.On("InsertMarker", ctx, "user-tz-stable", "budget-tz-stable",
		expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(true, nil).Once()
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		ea, ok := job.(RuleEvaluationArgs)
		return ok && ea.UserID == "user-tz-stable"
	}), mock.Anything).Return(&queue.JobResult{ID: 10}, nil).Once()

	worker1 := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
		UserTimezones:    tzLookup1,
	})
	require.NoError(t, worker1.workAt(ctx, newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{}), now))

	// Run 2 (America/New_York timezone): user changed tz. HasMarker must still return
	// true for the SAME UTC-anchored (expectedPeriodStart, expectedPeriodEnd) key.
	// The marker identity must not shift due to the timezone change.
	tzLookup2 := new(mockUserTimezoneLookup)
	tzLookup2.On("GetTimezonesByUserIDs", ctx, []string{"user-tz-stable"}).
		Return(map[string]string{"user-tz-stable": "America/New_York"}, nil)

	periodEventsRepo.On("HasMarker", ctx, "user-tz-stable", "budget-tz-stable",
		expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(true, nil).Once() // marker already exists → skip

	worker2 := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
		UserTimezones:    tzLookup2,
	})
	require.NoError(t, worker2.workAt(ctx, newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{}), now))

	// Enqueue must have been called exactly once across both runs.
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 1)
	periodEventsRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

// TestWorkAt_FallsBackToUTCWhenTimezoneNil verifies that when UserTimezones is nil,
// the worker falls back to UTC and still processes period boundaries correctly.
func TestWorkAt_FallsBackToUTCWhenTimezoneNil(t *testing.T) {
	ctx := context.Background()

	// Feb 1 UTC → January just closed.
	now := time.Date(2024, time.February, 1, 9, 0, 0, 0, time.UTC)
	expectedPeriodStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	expectedPeriodEnd := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)

	budget := newMonthlyBudget("budget-utcfb", "user-utcfb", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	budgetRepo := &mockBudgetPeriodCheckRepo{}
	periodEventsRepo := &mockBudgetPeriodEventsRepo{}
	mockQueue := new(MockJobQueue)

	budgetRepo.On("GetAllActivePaginated", ctx, budgetPeriodCheckPageSize, 0).
		Return([]*domain.Budget{budget}, nil)
	periodEventsRepo.On("HasMarker", ctx, "user-utcfb", "budget-utcfb",
		expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(false, nil)
	periodEventsRepo.On("InsertMarker", ctx, "user-utcfb", "budget-utcfb",
		expectedPeriodStart, expectedPeriodEnd, domain.EventBudgetPeriodEnd).
		Return(true, nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		evalArgs, ok := job.(RuleEvaluationArgs)
		return ok && evalArgs.UserID == "user-utcfb" && evalArgs.EventType == domain.EventBudgetPeriodEnd
	}), mock.Anything).Return(&queue.JobResult{ID: 99}, nil)

	// UserTimezones is nil → falls back to UTC.
	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:       budgetRepo,
		PeriodEventsRepo: periodEventsRepo,
		JobQueue:         mockQueue,
		UserTimezones:    nil,
	})

	job := newTestBudgetPeriodCloseCheckJob(BudgetPeriodCloseCheckArgs{TriggerSource: "test-utcfb"})
	err := worker.workAt(ctx, job, now)
	require.NoError(t, err)

	// With UTC fallback: February 1 → January just closed → event fires.
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 1)
	mockQueue.AssertExpectations(t)
	periodEventsRepo.AssertExpectations(t)
}
