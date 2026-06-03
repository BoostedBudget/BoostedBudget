package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockRuleExecutionService mocks the RuleExecutionService interface
type MockRuleExecutionService struct {
	mock.Mock
}

func (m *MockRuleExecutionService) ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleExecution), args.Error(1)
}

func (m *MockRuleExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
}

func (m *MockRuleExecutionService) GetExecutionHistory(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error) {
	args := m.Called(ctx, ruleID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Error(1)
}

func (m *MockRuleExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error) {
	args := m.Called(ctx, ruleID, status, limit, offset)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Int(1), args.Error(2)
}

func (m *MockRuleExecutionService) TestRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleTestResult, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleTestResult), args.Error(1)
}

func newTestRuleEvaluationJob(args RuleEvaluationArgs) *river.Job[RuleEvaluationArgs] {
	return &river.Job[RuleEvaluationArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestRuleEvaluationArgs_Kind(t *testing.T) {
	args := RuleEvaluationArgs{}
	assert.Equal(t, "rule_evaluation", args.Kind())
}

func TestRuleEvaluationWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	eventType := "budget_updated"
	eventData := map[string]interface{}{
		"budget_id":    uuid.New().String(),
		"budget_name":  "Groceries",
		"percent_used": 85.0,
	}

	mockService := new(MockRuleExecutionService)
	mockService.On("ProcessEventTrigger", ctx, userID, eventType, eventData).Return(nil)

	worker := NewRuleEvaluationWorker(RuleEvaluationWorkerDeps{
		RuleExecutionService: mockService,
		Logger:               slog.Default(),
	})

	job := newTestRuleEvaluationJob(RuleEvaluationArgs{
		UserID:    userID,
		EventType: eventType,
		EventData: eventData,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

func TestRuleEvaluationWorker_Work_ServiceError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	eventType := "budget_updated"
	eventData := map[string]interface{}{}

	mockService := new(MockRuleExecutionService)
	mockService.On("ProcessEventTrigger", ctx, userID, eventType, eventData).
		Return(errors.New("database connection error"))

	worker := NewRuleEvaluationWorker(RuleEvaluationWorkerDeps{
		RuleExecutionService: mockService,
		Logger:               slog.Default(),
	})

	job := newTestRuleEvaluationJob(RuleEvaluationArgs{
		UserID:    userID,
		EventType: eventType,
		EventData: eventData,
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule evaluation failed")
	mockService.AssertExpectations(t)
}

func TestRuleEvaluationWorker_Work_TransactionCreatedEvent(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	eventType := "transaction_created"
	eventData := map[string]interface{}{
		"transaction_id": uuid.New().String(),
		"amount":         -42.50,
		"merchant_name":  "Coffee Shop",
		"category":       []string{"Food and Drink", "Coffee"},
	}

	mockService := new(MockRuleExecutionService)
	mockService.On("ProcessEventTrigger", ctx, userID, eventType, eventData).Return(nil)

	worker := NewRuleEvaluationWorker(RuleEvaluationWorkerDeps{
		RuleExecutionService: mockService,
		Logger:               slog.Default(),
	})

	job := newTestRuleEvaluationJob(RuleEvaluationArgs{
		UserID:    userID,
		EventType: eventType,
		EventData: eventData,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

func TestRuleEvaluationUniqueOpts(t *testing.T) {
	opts := RuleEvaluationUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 1*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
}

func TestNewRuleEvaluationWorker_DefaultLogger(t *testing.T) {
	mockService := new(MockRuleExecutionService)

	worker := NewRuleEvaluationWorker(RuleEvaluationWorkerDeps{
		RuleExecutionService: mockService,
		// Logger is nil
	})

	assert.NotNil(t, worker.logger)
}
