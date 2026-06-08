// Package jobs contains River job definitions and workers for async processing.
// Issue #1270: Task 4 — BudgetPeriodCloseCheck tests (TDD red phase).
package jobs

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mocks
// =============================================================================

// MockBudgetPeriodRepo mocks the BudgetPeriodRepository interface.
type MockBudgetPeriodRepo struct {
	mock.Mock
}

func (m *MockBudgetPeriodRepo) GetBudgetWithCategoriesByID(ctx context.Context, budgetID, userID string) (*domain.Budget, error) {
	args := m.Called(ctx, budgetID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Budget), args.Error(1)
}

// MockBudgetPeriodEventProcessor mocks the BudgetPeriodEventProcessor interface.
type MockBudgetPeriodEventProcessor struct {
	mock.Mock
}

func (m *MockBudgetPeriodEventProcessor) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
}

// =============================================================================
// Helpers
// =============================================================================

func newTestBudgetPeriodCloseJob(args BudgetPeriodCloseCheckArgs) *river.Job[BudgetPeriodCloseCheckArgs] {
	return &river.Job[BudgetPeriodCloseCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestBudgetPeriodCloseCheckArgs_Kind(t *testing.T) {
	args := BudgetPeriodCloseCheckArgs{}
	assert.Equal(t, "budget_period_close_check", args.Kind())
}

// TestBudgetPeriodCloseCheck_EmitsCategoriesInEventData verifies that the worker
// emits a "budget_period_end" event containing per-category data (Issue #1270 Task 4).
func TestBudgetPeriodCloseCheck_EmitsCategoriesInEventData(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	budgetID := uuid.New().String()

	pfcPrimary := "FOOD_AND_DRINK"
	budget := &domain.Budget{
		ID:     budgetID,
		UserID: userID,
		Name:   "Monthly Budget",
		Amount: 1000.0,
		Categories: []domain.BudgetCategory{
			{
				ID:              "cat-food",
				BudgetID:        budgetID,
				CategoryKind:    "pfc",
				PFCPrimary:      &pfcPrimary,
				AllocatedAmount: 400.0,
				SpentAmount:     200.0,
			},
		},
	}

	mockRepo := new(MockBudgetPeriodRepo)
	mockRepo.On("GetBudgetWithCategoriesByID", ctx, budgetID, userID).Return(budget, nil)

	var capturedEventData map[string]interface{}
	mockEvent := new(MockBudgetPeriodEventProcessor)
	mockEvent.On("ProcessEventTrigger", ctx, userID, domain.EventBudgetPeriodEnd, mock.AnythingOfType("map[string]interface {}")).
		Run(func(args mock.Arguments) {
			capturedEventData = args.Get(3).(map[string]interface{})
		}).
		Return(nil)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:     mockRepo,
		EventProcessor: mockEvent,
		Logger:         slog.Default(),
	})

	job := newTestBudgetPeriodCloseJob(BudgetPeriodCloseCheckArgs{
		BudgetID: budgetID,
		UserID:   userID,
	})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	// EventData must contain a "categories" key.
	require.NotNil(t, capturedEventData, "event data should be captured")
	categoriesRaw, ok := capturedEventData[domain.EventDataKeyCategories]
	require.True(t, ok, "event data must contain 'categories' key")

	catMap, ok := categoriesRaw.(map[string]interface{})
	require.True(t, ok, "categories value must be map[string]interface{}")
	require.Contains(t, catMap, "cat-food")

	perCat, ok := catMap["cat-food"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 50.0, perCat["percent_used"])   // 200/400*100
	assert.Equal(t, 200.0, perCat["remaining"])     // 400-200
	assert.Equal(t, 200.0, perCat["total_spent"])

	mockRepo.AssertExpectations(t)
	mockEvent.AssertExpectations(t)
}

// TestBudgetPeriodCloseCheck_RepoError verifies that a repo error fails the job.
func TestBudgetPeriodCloseCheck_RepoError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	budgetID := uuid.New().String()

	mockRepo := new(MockBudgetPeriodRepo)
	mockRepo.On("GetBudgetWithCategoriesByID", ctx, budgetID, userID).Return(nil, assert.AnError)

	mockEvent := new(MockBudgetPeriodEventProcessor)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:     mockRepo,
		EventProcessor: mockEvent,
		Logger:         slog.Default(),
	})

	job := newTestBudgetPeriodCloseJob(BudgetPeriodCloseCheckArgs{
		BudgetID: budgetID,
		UserID:   userID,
	})

	err := worker.Work(ctx, job)
	require.Error(t, err)
	mockEvent.AssertNotCalled(t, "ProcessEventTrigger")
}

// TestBudgetPeriodCloseCheck_NoEventProcessorIsNoop verifies that when no event
// processor is set, the worker succeeds without panicking.
func TestBudgetPeriodCloseCheck_NoEventProcessorIsNoop(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	budgetID := uuid.New().String()

	budget := &domain.Budget{
		ID:     budgetID,
		UserID: userID,
		Name:   "Monthly Budget",
		Amount: 500.0,
	}

	mockRepo := new(MockBudgetPeriodRepo)
	mockRepo.On("GetBudgetWithCategoriesByID", ctx, budgetID, userID).Return(budget, nil)

	worker := NewBudgetPeriodCloseCheckWorker(BudgetPeriodCloseCheckWorkerDeps{
		BudgetRepo:     mockRepo,
		EventProcessor: nil, // No event processor
		Logger:         slog.Default(),
	})

	job := newTestBudgetPeriodCloseJob(BudgetPeriodCloseCheckArgs{
		BudgetID: budgetID,
		UserID:   userID,
	})

	err := worker.Work(ctx, job)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}
