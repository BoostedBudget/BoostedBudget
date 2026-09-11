// Package services provides tests for boost execution service.
// Issue #35: Rules Engine Core - execution with condition evaluation.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock repositories for testing.

type mockBoostExecutionRepository struct {
	executions  []*domain.BoostExecution
	createError error
	updateError error
}

func (m *mockBoostExecutionRepository) Create(ctx context.Context, execution *domain.BoostExecution) error {
	if m.createError != nil {
		return m.createError
	}
	m.executions = append(m.executions, execution)
	return nil
}

func (m *mockBoostExecutionRepository) Update(ctx context.Context, execution *domain.BoostExecution) error {
	if m.updateError != nil {
		return m.updateError
	}
	for i, e := range m.executions {
		if e.ID == execution.ID {
			m.executions[i] = execution
			return nil
		}
	}
	return errors.New("execution not found")
}

func (m *mockBoostExecutionRepository) GetByID(ctx context.Context, id string) (*domain.BoostExecution, error) {
	for _, e := range m.executions {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockBoostExecutionRepository) GetByBoostID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.BoostExecution, error) {
	var result []*domain.BoostExecution
	for _, e := range m.executions {
		if e.BoostID != nil && *e.BoostID == ruleID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockBoostExecutionRepository) GetLastExecutionForBoost(ctx context.Context, ruleID string) (*domain.BoostExecution, error) {
	var latest *domain.BoostExecution
	for _, e := range m.executions {
		if e.BoostID != nil && *e.BoostID == ruleID {
			if latest == nil || e.StartedAt.After(latest.StartedAt) {
				latest = e
			}
		}
	}
	if latest == nil {
		return nil, errors.New("not found")
	}
	return latest, nil
}

func (m *mockBoostExecutionRepository) GetPendingExecutions(ctx context.Context, limit int) ([]*domain.BoostExecution, error) {
	var result []*domain.BoostExecution
	for _, e := range m.executions {
		if e.Status == domain.ExecutionStatusPending {
			result = append(result, e)
		}
	}
	return result, nil
}

// GetByUserID is the Issue #1569 Task 14 reader that can see an execution whose boost has been
// purged (boost_id NULL). Matching on the execution's own UserID mirrors the real query.
func (m *mockBoostExecutionRepository) GetByUserID(ctx context.Context, userID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	var result []*domain.BoostExecution
	for _, e := range m.executions {
		if e.UserID != userID {
			continue
		}
		if status != nil && e.Status != *status {
			continue
		}
		result = append(result, e)
	}
	return result, len(result), nil
}

func (m *mockBoostExecutionRepository) GetFailedExecutions(ctx context.Context, ruleID string, limit int) ([]*domain.BoostExecution, error) {
	var result []*domain.BoostExecution
	for _, e := range m.executions {
		if e.BoostID != nil && *e.BoostID == ruleID && e.Status == domain.ExecutionStatusFailed {
			result = append(result, e)
		}
	}
	return result, nil
}

// GetByBoostIDWithFilter implements BoostExecutionRepository with optional status filter.
// Issue #38: Rules API Endpoints - Execution history with filtering.
func (m *mockBoostExecutionRepository) GetByBoostIDWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	var result []*domain.BoostExecution
	for _, e := range m.executions {
		if e.BoostID != nil && *e.BoostID == ruleID {
			if status == nil || e.Status == *status {
				result = append(result, e)
			}
		}
	}

	total := len(result)

	// Apply pagination
	if offset >= len(result) {
		return []*domain.BoostExecution{}, total, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}

	return result, total, nil
}

type mockTransferSuggestionRepository struct {
	suggestions []*domain.TransferSuggestion
	createError error
}

func (m *mockTransferSuggestionRepository) Create(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	if m.createError != nil {
		return m.createError
	}
	m.suggestions = append(m.suggestions, suggestion)
	return nil
}

func (m *mockTransferSuggestionRepository) GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error) {
	for _, s := range m.suggestions {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error) {
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.UserID == userID {
			if status == nil || s.Status == *status {
				result = append(result, s)
			}
		}
	}
	return result, nil
}

func (m *mockTransferSuggestionRepository) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	status := domain.SuggestionStatusPending
	return m.GetByUserID(ctx, userID, &status, 100, 0)
}

func (m *mockTransferSuggestionRepository) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	for _, s := range m.suggestions {
		if s.ID == id {
			s.Status = status
			s.UserNotes = userNotes
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) CancelAcceptedSuggestion(ctx context.Context, id string, userNotes *string) error {
	for _, s := range m.suggestions {
		if s.ID == id && (s.Status == domain.SuggestionStatusPending || s.Status == domain.SuggestionStatusAccepted) {
			s.Status = domain.SuggestionStatusRejected
			s.UserNotes = userNotes
			return nil
		}
	}
	return domain.ErrTransferSuggestionAlreadyActioned
}

func (m *mockTransferSuggestionRepository) Update(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	for i, s := range m.suggestions {
		if s.ID == suggestion.ID {
			m.suggestions[i] = suggestion
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) ExpirePending(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockTransferSuggestionRepository) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, s := range m.suggestions {
		if s.UserID == userID && s.Status == domain.SuggestionStatusPending {
			count++
		}
	}
	return count, nil
}

func (m *mockTransferSuggestionRepository) GetByBoostID(ctx context.Context, boostID string, limit, offset int) ([]*domain.TransferSuggestion, error) {
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.BoostID != nil && *s.BoostID == boostID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockTransferSuggestionRepository) Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error {
	for _, s := range m.suggestions {
		if s.ID == id {
			if s.Status != domain.SuggestionStatusPending && s.Status != domain.SuggestionStatusAccepted {
				return domain.ErrTransferSuggestionAlreadyActioned
			}
			s.Status = domain.SuggestionStatusCompleted
			s.MatchedTransactionID = matchedTransactionID
			s.ActualAmount = &actualAmount
			s.CompletionSource = &completionSource
			s.CompletedAt = &completedAt
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.UserID == userID && (s.Status == domain.SuggestionStatusPending || s.Status == domain.SuggestionStatusAccepted) {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockTransferSuggestionRepository) ReserveMatchedTransaction(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockTransferSuggestionRepository) ClearMatchedTransactionID(_ context.Context, _ string, _ *string) error {
	return nil
}

func (m *mockTransferSuggestionRepository) CompleteRetainingReservation(ctx context.Context, id string, actualAmount float64, completionSource string, completedAt time.Time, expectedTxnID *string) error {
	for _, s := range m.suggestions {
		if s.ID == id {
			if s.Status != domain.SuggestionStatusPending && s.Status != domain.SuggestionStatusAccepted {
				return domain.ErrTransferSuggestionAlreadyActioned
			}
			// GAP AR: check IS NOT DISTINCT FROM semantics (both nil, or same value)
			if (expectedTxnID == nil) != (s.MatchedTransactionID == nil) {
				return domain.ErrTransferSuggestionAlreadyActioned
			}
			if expectedTxnID != nil && s.MatchedTransactionID != nil && *expectedTxnID != *s.MatchedTransactionID {
				return domain.ErrTransferSuggestionAlreadyActioned
			}
			s.Status = domain.SuggestionStatusCompleted
			s.ActualAmount = &actualAmount
			s.CompletionSource = &completionSource
			s.CompletedAt = &completedAt
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) ClearStaleReservations(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockTransferSuggestionRepository) GetByUserIDStatuses(ctx context.Context, userID string, statuses []domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, int, error) {
	statusSet := make(map[domain.SuggestionStatus]bool, len(statuses))
	for _, s := range statuses {
		statusSet[s] = true
	}
	if len(statusSet) == 0 {
		statusSet[domain.SuggestionStatusPending] = true
	}
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.UserID == userID && statusSet[s.Status] {
			result = append(result, s)
		}
	}
	total := len(result)
	// Apply pagination.
	if offset < len(result) {
		result = result[offset:]
	} else {
		result = nil
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, total, nil
}

// TestConditionEvaluation tests the condition matching logic.
func TestConditionEvaluation(t *testing.T) {
	t.Run("evaluates equals operator", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorEq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates not_equals operator", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorNeq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates contains operator", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorContains,
			Value:    "mart",
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates greater_than operator with numbers", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorGt,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 150.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates less_than operator with numbers", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorLt,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 50.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates greater_than_or_equal operator", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorGte,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates less_than_or_equal operator", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorLte,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("returns false for missing field", func(t *testing.T) {
		condition := domain.BoostCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorEq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.False(t, result)
	})
}

// TestConditionLogic tests AND/OR condition logic.
func TestConditionLogic(t *testing.T) {
	t.Run("AND logic requires all conditions to match", func(t *testing.T) {
		conditions := []domain.BoostCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
			"amount":        150.0,
		}

		result := evaluateConditions(conditions, "AND", data)
		assert.True(t, result)
	})

	t.Run("AND logic fails if any condition fails", func(t *testing.T) {
		conditions := []domain.BoostCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
			"amount":        50.0, // Less than 100
		}

		result := evaluateConditions(conditions, "AND", data)
		assert.False(t, result)
	})

	t.Run("OR logic succeeds if any condition matches", func(t *testing.T) {
		conditions := []domain.BoostCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart", // Not Amazon
			"amount":        150.0,     // Greater than 100
		}

		result := evaluateConditions(conditions, "OR", data)
		assert.True(t, result)
	})

	t.Run("OR logic fails if all conditions fail", func(t *testing.T) {
		conditions := []domain.BoostCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart", // Not Amazon
			"amount":        50.0,      // Less than 100
		}

		result := evaluateConditions(conditions, "OR", data)
		assert.False(t, result)
	})

	t.Run("empty conditions always match", func(t *testing.T) {
		data := map[string]interface{}{
			"merchant_name": "Amazon",
		}

		result := evaluateConditions([]domain.BoostCondition{}, "AND", data)
		assert.True(t, result)
	})
}

// TestBoostExecutionService tests the boost execution service.
func TestBoostExecutionService(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	createTestBoost := func(boostType domain.BoostType, triggerType domain.TriggerType) *domain.Boost {
		var actionsJSON []byte
		switch boostType {
		case domain.BoostTypeTransferSuggestion:
			actionsJSON, _ = json.Marshal(domain.TransferAction{
				FromAccountID: "account-1",
				ToAccountID:   "account-2",
				AmountType:    "fixed",
				Amount:        100.0,
			})
		case domain.BoostTypeBudgetAlert:
			actionsJSON, _ = json.Marshal(domain.AlertAction{
				ThresholdType:  "percentage",
				ThresholdValue: 80,
			})
		case domain.BoostTypeCategoryRollover:
			actionsJSON, _ = json.Marshal(domain.RolloverAction{
				RolloverType: "all",
			})
		default:
			actionsJSON = json.RawMessage(`{}`)
		}

		return &domain.Boost{
			ID:        "rule-1",
			UserID:    userID,
			Name:      "Test Rule",
			Enabled:   true,
			Priority:  1,
			BoostType: boostType,
			TriggerConfig: domain.TriggerConfig{
				Type:  triggerType,
				Event: "transaction_created",
			},
			Conditions: []domain.BoostCondition{
				{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("executes transfer suggestion rule successfully", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil, // notification service (not needed for transfer)
		)

		boost := createTestBoost(domain.BoostTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.NotEmpty(t, execution.ID)
		require.NotNil(t, execution.BoostID)
		assert.Equal(t, boost.ID, *execution.BoostID)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)
		assert.NotNil(t, execution.CompletedAt)

		// Verify suggestion was created
		assert.Len(t, suggestionRepo.suggestions, 1)
		suggestion := suggestionRepo.suggestions[0]
		assert.Equal(t, userID, suggestion.UserID)
		assert.NotNil(t, suggestion.BoostID)
		assert.Equal(t, boost.ID, *suggestion.BoostID)
		assert.Equal(t, 100.0, suggestion.Amount)
	})

	t.Run("skips execution when conditions don't match", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestBoost(domain.BoostTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 25.0, // Less than 50, condition won't match
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, ExecutionStatusSkipped, execution.Status)

		// Verify no suggestion was created
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("handles disabled rule", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestBoost(domain.BoostTypeTransferSuggestion, domain.TriggerTypeEvent)
		boost.Enabled = false

		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, ExecutionStatusSkipped, execution.Status)
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("records failed execution on action error", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{
			createError: errors.New("database error"),
		}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestBoost(domain.BoostTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.Error(t, err)
		assert.Equal(t, domain.ExecutionStatusFailed, execution.Status)
		assert.NotEmpty(t, execution.ErrorMessage)
	})
}

// TestProcessEventTrigger tests event trigger processing.
func TestProcessEventTrigger(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	t.Run("processes event trigger for matching rules", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}

		actionsJSON, _ := json.Marshal(domain.TransferAction{
			FromAccountID: "account-1",
			ToAccountID:   "account-2",
			AmountType:    "fixed",
			Amount:        100.0,
		})

		boostRepo := &mockBoostsRepository{
			boosts: []*domain.Boost{
				{
					ID:        "rule-1",
					UserID:    userID,
					Name:      "Test Rule",
					Enabled:   true,
					Priority:  1,
					BoostType: domain.BoostTypeTransferSuggestion,
					TriggerConfig: domain.TriggerConfig{
						Type:  domain.TriggerTypeEvent,
						Event: "transaction_created",
					},
					Conditions:     []domain.BoostCondition{},
					ConditionLogic: "AND",
					Actions:        actionsJSON,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		require.NoError(t, err)
		assert.Len(t, suggestionRepo.suggestions, 1)
	})

	t.Run("processes event with no matching rules", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{
			// No rules configured
			boosts: []*domain.Boost{},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		// Should succeed even with no matching rules
		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		assert.NoError(t, err)
		// No suggestions created since no rules matched
		assert.Empty(t, suggestionRepo.suggestions)
	})

}

// TestGetExecutionHistory tests execution history retrieval.
func TestGetExecutionHistory(t *testing.T) {
	ctx := context.Background()

	t.Run("retrieves execution history for boost", func(t *testing.T) {
		now := time.Now()
		execRepo := &mockBoostExecutionRepository{
			executions: []*domain.BoostExecution{
				{
					ID:        "exec-1",
					BoostID:   strPtr("rule-1"),
					Status:    domain.ExecutionStatusCompleted,
					StartedAt: now.Add(-1 * time.Hour),
				},
				{
					ID:        "exec-2",
					BoostID:   strPtr("rule-1"),
					Status:    domain.ExecutionStatusCompleted,
					StartedAt: now,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			nil,
			nil,
			nil,
		)

		executions, err := service.GetBoostExecutionHistory(ctx, "rule-1", 10, 0)

		require.NoError(t, err)
		assert.Len(t, executions, 2)
	})
}

// Mock boosts repository for testing.
type mockBoostsRepository struct {
	boosts              []*domain.Boost
	createError         error
	eventTriggerBoosts  []*domain.Boost
	incrementExecCalled bool
	updateSuccessCalled bool
}

func (m *mockBoostsRepository) Create(ctx context.Context, boost *domain.Boost) error {
	if m.createError != nil {
		return m.createError
	}
	m.boosts = append(m.boosts, boost)
	return nil
}

func (m *mockBoostsRepository) GetByID(ctx context.Context, id string) (*domain.Boost, error) {
	for _, r := range m.boosts {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrBoostNotFound
}

func (m *mockBoostsRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.UserID == userID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockBoostsRepository) GetByUserIDFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	if len(boostTypes) == 0 {
		return m.GetByUserID(ctx, userID)
	}
	typeSet := make(map[domain.BoostType]bool)
	for _, t := range boostTypes {
		typeSet[t] = true
	}
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.UserID == userID && typeSet[r.BoostType] {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockBoostsRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.UserID == userID && r.Enabled {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockBoostsRepository) GetByName(ctx context.Context, userID, name string) (*domain.Boost, error) {
	for _, r := range m.boosts {
		if r.UserID == userID && r.Name == name {
			return r, nil
		}
	}
	return nil, domain.ErrBoostNotFound
}

func (m *mockBoostsRepository) Update(ctx context.Context, boost *domain.Boost) error {
	for i, r := range m.boosts {
		if r.ID == boost.ID {
			m.boosts[i] = boost
			return nil
		}
	}
	return domain.ErrBoostNotFound
}

func (m *mockBoostsRepository) Delete(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
	for i, r := range m.boosts {
		if r.ID == id {
			m.boosts = append(m.boosts[:i], m.boosts[i+1:]...)
			return nil
		}
	}
	return domain.ErrBoostNotFound
}

func (m *mockBoostsRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, r := range m.boosts {
		if r.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (m *mockBoostsRepository) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	m.incrementExecCalled = true
	return nil
}

func (m *mockBoostsRepository) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	m.updateSuccessCalled = true
	return nil
}

func (m *mockBoostsRepository) GetBoostsByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.Boost, error) {
	if m.eventTriggerBoosts != nil {
		return m.eventTriggerBoosts, nil
	}
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.UserID == userID && r.Enabled && r.TriggerConfig.Type == domain.TriggerTypeEvent && r.TriggerConfig.Event == eventType {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockBoostsRepository) GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error) {
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.UserID == userID && r.Enabled && r.BoostType == boostType {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockBoostsRepository) GetBoostsByScheduleTrigger(ctx context.Context) ([]*domain.Boost, error) {
	var result []*domain.Boost
	for _, r := range m.boosts {
		if r.Enabled && r.TriggerConfig.Type == domain.TriggerTypeSchedule && r.TriggerConfig.Cron != "" {
			result = append(result, r)
		}
	}
	return result, nil
}

// ============================================================================
// Issue #36: Savings Goal Executor Tests (TDD)
// ============================================================================

// mockSavingsGoalRepository for testing savings goal executor.
type mockSavingsGoalRepository struct {
	goals     []*domain.SavingsGoal
	findError error
}

func (m *mockSavingsGoalRepository) Create(ctx context.Context, goal *domain.SavingsGoal) error {
	m.goals = append(m.goals, goal)
	return nil
}

func (m *mockSavingsGoalRepository) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	if m.findError != nil {
		return nil, m.findError
	}
	for _, g := range m.goals {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, domain.ErrSavingsGoalNotFound
}

// GetByIDForUser retrieves a savings goal by ID with ownership validation (GAP AE).
func (m *mockSavingsGoalRepository) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	if m.findError != nil {
		return nil, m.findError
	}
	for _, g := range m.goals {
		if g.ID == id && g.UserID == userID {
			return g, nil
		}
	}
	return nil, domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) GetDeletedByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return nil, nil
}

func (m *mockSavingsGoalRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.UserID == userID {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.UserID == userID && !g.IsCompleted {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	for _, g := range m.goals {
		if g.UserID == userID && g.Name == name {
			return g, nil
		}
	}
	return nil, domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) BeginTx(ctx context.Context) (repository.Transaction, error) {
	return nil, errors.New("mockSavingsGoalRepository: BeginTx not supported")
}

func (m *mockSavingsGoalRepository) UpdateWithBasisGuardInTx(ctx context.Context, tx repository.Transaction, goal *domain.SavingsGoal, priorBasis string) error {
	return errors.New("mockSavingsGoalRepository: UpdateWithBasisGuardInTx not supported")
}

func (m *mockSavingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	for i, g := range m.goals {
		if g.ID == goal.ID {
			m.goals[i] = goal
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) PurgeGoal(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
	return m.Delete(ctx, id, userID, auditLog)
}

func (m *mockSavingsGoalRepository) Delete(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
	for i, g := range m.goals {
		if g.ID == id {
			m.goals = append(m.goals[:i], m.goals[i+1:]...)
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, g := range m.goals {
		if g.UserID == userID {
			count++
		}
	}
	return count, nil
}

// Issue #1569 Task 20 (A4): completed goals live outside GetActiveByUserID's universe, so the
// summary counts them separately. This fake mirrors CountByUserID's shape.
func (m *mockSavingsGoalRepository) CountCompletedByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, g := range m.goals {
		if g.UserID == userID && g.IsCompleted {
			count++
		}
	}
	return count, nil
}

func (m *mockSavingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.AccountID == accountID {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	for _, g := range m.goals {
		if g.ID == goalID {
			g.CurrentAmount = currentAmount
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	for _, g := range m.goals {
		if g.ID == goalID {
			g.IsCompleted = true
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

// TestSavingsGoalExecutor tests the savings goal rule executor.
func TestSavingsGoalExecutor(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	goalID := "savings-goal-123"
	accountID := "savings-account-456"
	fromAccountID := "checking-account-789"

	createSavingsGoalBoost := func(action domain.SavingsGoalAction) *domain.Boost {
		actionsJSON, _ := json.Marshal(action)

		return &domain.Boost{
			ID:        "rule-savings-1",
			UserID:    userID,
			Name:      "Savings Goal Contribution",
			Enabled:   true,
			Priority:  1,
			BoostType: domain.BoostTypeSavingsGoal,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions:     []domain.BoostCondition{},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("executes savings goal rule with fixed amount", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "Emergency Fund",
					AccountID:     accountID,
					TargetAmount:  10000.0,
					CurrentAmount: 2500.0,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "fixed",
			Amount:        200.0,
		}
		boost := createSavingsGoalBoost(action)

		triggerData := map[string]interface{}{
			"amount": 1000.0, // Paycheck
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		// Verify suggestion was created
		require.Len(t, suggestionRepo.suggestions, 1)
		suggestion := suggestionRepo.suggestions[0]
		assert.Equal(t, userID, suggestion.UserID)
		assert.Equal(t, fromAccountID, *suggestion.FromAccountID)
		assert.Equal(t, accountID, *suggestion.ToAccountID) // Savings goal's linked account
		assert.Equal(t, 200.0, suggestion.Amount)           // Fixed amount
		assert.Contains(t, *suggestion.Reason, "Emergency Fund")
	})

	t.Run("executes savings goal rule with percentage amount", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "Vacation Fund",
					AccountID:     accountID,
					TargetAmount:  5000.0,
					CurrentAmount: 1000.0,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		percentage := 10.0
		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "percentage",
			Percentage:    &percentage,
		}
		boost := createSavingsGoalBoost(action)

		triggerData := map[string]interface{}{
			"amount": 500.0, // 10% of 500 = 50
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		require.Len(t, suggestionRepo.suggestions, 1)
		assert.Equal(t, 50.0, suggestionRepo.suggestions[0].Amount) // 10% of 500
	})

	t.Run("executes savings goal rule with remaining amount", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "New Car Fund",
					AccountID:     accountID,
					TargetAmount:  20000.0,
					CurrentAmount: 5000.0,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "remaining",
			Threshold:     1000.0, // Keep at least $1000, transfer rest
		}
		boost := createSavingsGoalBoost(action)

		triggerData := map[string]interface{}{
			"remaining": 1500.0, // Remaining budget amount after spending (EventDataKeyRemaining)
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		require.Len(t, suggestionRepo.suggestions, 1)
		assert.Equal(t, 500.0, suggestionRepo.suggestions[0].Amount) // 1500 - 1000 = 500
	})

	t.Run("fails when savings goal action is missing", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		// Create boost without savings goal action — ExecuteBoost now validates first via
		// boost.Validate(), so the error is caught before the execution record is created.
		// Use "budget_period_end" (a valid event) so that event-name validation passes and
		// Validate() proceeds to check savings_goal_id.
		actionsJSON := json.RawMessage(`{}`)
		boost := &domain.Boost{
			ID:             "rule-savings-bad",
			UserID:         userID,
			Name:           "Bad Savings Rule",
			Enabled:        true,
			BoostType:      domain.BoostTypeSavingsGoal,
			TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}

		execution, err := service.ExecuteBoost(ctx, boost, map[string]interface{}{})

		require.Error(t, err)
		// boost.Validate() catches the missing savings_goal_id before the executor runs.
		assert.Contains(t, err.Error(), "savings_goal_id is required")
		// Execution record is not created when pre-flight validation fails.
		assert.Nil(t, execution)
	})

	t.Run("skips silently when savings goal not found or ownership mismatch (GAP AE)", func(t *testing.T) {
		// Post-GAP AE behavior: executor skips (log+nil) instead of failing with an error.
		// This prevents IDOR information leakage: both "not found" and "belongs to other user"
		// return the same no-op result so an attacker can't distinguish them via error messages.
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{}, // Empty - goal not found
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: "non-existent-goal",
			FromAccountID: fromAccountID,
			AmountType:    "fixed",
			Amount:        100.0,
		}
		boost := createSavingsGoalBoost(action)

		execution, err := service.ExecuteBoost(ctx, boost, map[string]interface{}{})

		// Boost is skipped (no error bubbled up — avoids leaking existence info).
		require.NoError(t, err, "goal-not-found must be silently skipped, not returned as error")
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status, "execution is marked completed (skip-with-no-suggestion)")
		// Critical: no suggestion must be created for the non-owner rule.
		assert.Empty(t, suggestionRepo.suggestions, "no transfer suggestion must be created when goal is not found")
	})

	t.Run("skips when calculated amount is zero or negative", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:        goalID,
					UserID:    userID,
					Name:      "Test Fund",
					AccountID: accountID,
				},
			},
		}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "remaining",
			Threshold:     1000.0,
		}
		boost := createSavingsGoalBoost(action)

		triggerData := map[string]interface{}{
			"remaining": 500.0, // Less than threshold (EventDataKeyRemaining)
		}

		execution, err := service.ExecuteBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)
		// No suggestion created when amount <= 0
		assert.Empty(t, suggestionRepo.suggestions)
	})
}

// ============================================================================
// GAP H: SetSavingsGoalRepo nil guard — ensures production wiring is correct.
// ============================================================================

// TestBoostExecutionService_SavingsGoal_NilRepo_ReturnsError verifies that when
// SetSavingsGoalRepo is never called (i.e. the production main.go omits it),
// ProcessEventTrigger with a savings_goal boost returns a meaningful error instead
// of silently succeeding with no suggestion created.
func TestBoostExecutionService_SavingsGoal_NilRepo_ReturnsError(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	// Return a savings_goal boost when queried for budget_period_end events.
	actionsJSON, _ := json.Marshal(domain.SavingsGoalAction{
		SavingsGoalID: "goal-123",
		FromAccountID: "checking-account",
		AmountType:    "fixed",
		Amount:        100.0,
	})
	boost := &domain.Boost{
		ID:             "rule-savings-nil-repo",
		UserID:         userID,
		Name:           "Savings Goal Rule",
		Enabled:        true,
		BoostType:      domain.BoostTypeSavingsGoal,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: domain.EventBudgetPeriodEnd},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
	}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	// Create service WITHOUT calling SetSavingsGoalRepo — simulates production misconfiguration.
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	eventData := map[string]interface{}{
		"budget_id": "budget-1",
		"remaining": 500.0,
	}
	err := svc.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)

	// ProcessEventTrigger swallows per-rule errors and returns nil. The execution
	// record must reflect a failed status with the "not configured" message.
	require.NoError(t, err)
	require.Len(t, execRepo.executions, 1, "an execution record should have been created")
	exec := execRepo.executions[0]
	assert.Equal(t, domain.ExecutionStatusFailed, exec.Status)
	require.NotNil(t, exec.ErrorMessage)
	assert.Contains(t, *exec.ErrorMessage, "savings goal repository not configured")

	// No suggestion should have been created.
	assert.Empty(t, suggestionRepo.suggestions)
}

// TestProcessEventTrigger_BatchContinuesAfterBoostFailure verifies Issue #972 B2:
// ProcessEventTrigger must continue executing remaining boosts when one boost fails,
// and must log the failure via slog.Error without aborting the batch.
// The test seeds TWO boosts: boost A fails (nil savingsGoalRepo), boost B succeeds
// (fixed transfer with all repos). Asserts boost B's suggestion is created despite
// boost A's error, and ProcessEventTrigger returns nil (batch success).
func TestProcessEventTrigger_BatchContinuesAfterBoostFailure(t *testing.T) {
	ctx := context.Background()
	userID := "user-batch-test"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	// Boost A: savings_goal boost that will fail (no savingsGoalRepo).
	actionsFailJSON, _ := json.Marshal(domain.SavingsGoalAction{
		SavingsGoalID: "goal-xyz",
		FromAccountID: "acct-a",
		AmountType:    "fixed",
		Amount:        50.0,
	})
	boostA := &domain.Boost{
		ID:             "rule-fail",
		UserID:         userID,
		Name:           "Failing Rule",
		Enabled:        true,
		BoostType:      domain.BoostTypeSavingsGoal,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: domain.EventBudgetPeriodEnd},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsFailJSON,
	}

	// Boost B: transfer boost that will succeed.
	actionsSucceedJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "acct-from",
		ToAccountID:   "acct-to",
		AmountType:    "fixed",
		Amount:        75.0,
	})
	boostB := &domain.Boost{
		ID:             "rule-succeed",
		UserID:         userID,
		Name:           "Succeeding Rule",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: domain.EventBudgetPeriodEnd},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsSucceedJSON,
	}

	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boostA, boostB}}

	// Service WITHOUT savingsGoalRepo (boost A will fail) but with suggestion support (boost B succeeds).
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID:    "budget-1",
		domain.EventDataKeyRemaining:   500.0,
		domain.EventDataKeyAmount:      1000.0,
		domain.EventDataKeyPercentUsed: 50.0,
	}
	err := svc.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)

	// ProcessEventTrigger must return nil — one boost failing must not abort the batch.
	require.NoError(t, err, "batch must succeed even when one boost fails")

	// Boost B's transfer suggestion must have been created despite boost A's failure.
	assert.NotEmpty(t, suggestionRepo.suggestions,
		"boost B's transfer suggestion must be created even though boost A failed")

	// Two execution records: one failed (boost A), one succeeded (boost B).
	assert.Len(t, execRepo.executions, 2,
		"both boosts must have execution records regardless of boost A's failure")
}

// ============================================================================
// GAP G: calculateSavingsGoalAmount uses wrong EventData key for "remaining".
// ============================================================================

// TestCalculateSavingsGoalAmount_Remaining_UsesRemainingKey verifies that the
// "remaining" AmountType reads from the "remaining" EventData key (emitted by
// budget_period_close_check.go), NOT the "balance" key that was previously used.
func TestCalculateSavingsGoalAmount_Remaining_UsesRemainingKey(t *testing.T) {
	svc := &boostExecutionService{}

	action := &domain.SavingsGoalAction{
		AmountType: "remaining",
		Threshold:  100.0, // keep at least $100, transfer the rest
	}

	// Use "remaining" key — the correct EventData key emitted by the period-close worker.
	triggerData := map[string]interface{}{
		"remaining": 150.0,
	}

	result := svc.calculateSavingsGoalAmount(action, triggerData)
	assert.Equal(t, 50.0, result, "150 - 100 threshold = 50; wrong key returns 0")
}

// TestCalculateSavingsGoalAmount_Remaining_BelowThreshold verifies no amount is
// returned when remaining is at or below the threshold.
func TestCalculateSavingsGoalAmount_Remaining_BelowThreshold(t *testing.T) {
	svc := &boostExecutionService{}

	action := &domain.SavingsGoalAction{
		AmountType: "remaining",
		Threshold:  200.0,
	}

	triggerData := map[string]interface{}{
		"remaining": 150.0, // below threshold — nothing to transfer
	}

	result := svc.calculateSavingsGoalAmount(action, triggerData)
	assert.Equal(t, 0.0, result)
}

// ============================================================================
// Issue #38: Test Boost Execution (TDD - Dry-Run)
// ============================================================================

// TestBoostTestExecution tests the dry-run boost execution for the test endpoint.
func TestBoostTestExecution(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	createTestTransferBoost := func() *domain.Boost {
		actionsJSON, _ := json.Marshal(domain.TransferAction{
			FromAccountID: "account-1",
			ToAccountID:   "account-2",
			AmountType:    "fixed",
			Amount:        100.0,
		})

		return &domain.Boost{
			ID:        "rule-1",
			UserID:    userID,
			Name:      "Test Transfer Rule",
			Enabled:   true,
			Priority:  1,
			BoostType: domain.BoostTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions: []domain.BoostCondition{
				{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	createTestAlertBoost := func() *domain.Boost {
		actionsJSON, _ := json.Marshal(domain.AlertAction{
			NotificationChannels: []string{"email", "in_app"},
			ThresholdType:        "percentage",
			ThresholdValue:       75.0,
		})

		return &domain.Boost{
			ID:        "rule-2",
			UserID:    userID,
			Name:      "Budget Alert Rule",
			Enabled:   true,
			Priority:  1,
			BoostType: domain.BoostTypeBudgetAlert,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "budget_updated",
			},
			Conditions: []domain.BoostCondition{
				{Field: "percent_used", Operator: domain.ConditionOperatorGte, Value: "75"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("returns test result with condition evaluations when conditions met", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		triggerData := map[string]interface{}{
			"amount": 100.0, // Greater than 50
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, boost.ID, result.BoostID)
		assert.Equal(t, boost.Name, result.BoostName)
		assert.Equal(t, boost.BoostType, result.BoostType)
		assert.True(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 1)
		assert.True(t, result.ConditionResults[0].Met)
		assert.Equal(t, "amount", result.ConditionResults[0].Field)
		assert.NotEmpty(t, result.SimulatedAction)
		assert.NotEmpty(t, result.TriggerDataUsed)
	})

	t.Run("returns test result with conditions not met", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		triggerData := map[string]interface{}{
			"amount": 25.0, // Less than 50, condition not met
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.False(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 1)
		assert.False(t, result.ConditionResults[0].Met)
		assert.Empty(t, result.SimulatedAction) // No action simulated when conditions not met
	})

	t.Run("does not persist execution record", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		_, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		// Verify no execution record was created
		assert.Empty(t, execRepo.executions)
	})

	t.Run("does not create transfer suggestions", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		_, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		// Verify no suggestions were created
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("works on disabled rules for preview", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		boost.Enabled = false // Disabled boost

		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		// Test should still work on disabled boosts
		assert.Equal(t, boost.ID, result.BoostID)
		assert.True(t, result.ConditionsMet)
	})

	t.Run("simulates budget alert action", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestAlertBoost()
		triggerData := map[string]interface{}{
			"percent_used": 82.5,
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet)
		assert.NotEmpty(t, result.SimulatedAction)
		// Check simulated action contains notification channels
		action, ok := result.SimulatedAction["notification_channels"]
		assert.True(t, ok)
		assert.NotNil(t, action)
	})

	t.Run("uses provided trigger data", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		triggerData := map[string]interface{}{
			"amount":       100.0,
			"custom_field": "custom_value",
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.Equal(t, triggerData, result.TriggerDataUsed)
	})

	t.Run("evaluates multiple conditions with AND logic", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		boost.Conditions = []domain.BoostCondition{
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			{Field: "merchant", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
		}
		boost.ConditionLogic = "AND"

		triggerData := map[string]interface{}{
			"amount":   100.0,
			"merchant": "Amazon",
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 2)
		assert.True(t, result.ConditionResults[0].Met)
		assert.True(t, result.ConditionResults[1].Met)
	})

	t.Run("evaluates multiple conditions with OR logic", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(
			execRepo,
			suggestionRepo,
			boostRepo,
			nil,
		)

		boost := createTestTransferBoost()
		boost.Conditions = []domain.BoostCondition{
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "500"},      // Will not match
			{Field: "merchant", Operator: domain.ConditionOperatorEq, Value: "Amazon"}, // Will match
		}
		boost.ConditionLogic = "OR"

		triggerData := map[string]interface{}{
			"amount":   100.0, // Less than 500
			"merchant": "Amazon",
		}

		result, err := service.TestBoost(ctx, boost, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet) // OR logic - at least one matches
		assert.Len(t, result.ConditionResults, 2)
	})
}

// TestBoostTestExecution_PFCConditionPopulatesDisplay verifies that TestBoost wires up
// PopulatePFCDisplay so value_display and actual_display appear in the response.
func TestBoostTestExecution_PFCConditionPopulatesDisplay(t *testing.T) {
	ctx := context.Background()

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	actions := json.RawMessage(`{}`)
	boost := &domain.Boost{
		ID:   "rule-pfc-test",
		Name: "PFC dry-run",
		Conditions: []domain.BoostCondition{
			{Field: "transaction.pfc_primary", Operator: domain.ConditionOperatorEq, Value: "FOOD_AND_DRINK"},
		},
		ConditionLogic: "AND",
		Actions:        actions,
	}
	triggerData := map[string]interface{}{
		"transaction.pfc_primary": "FOOD_AND_DRINK",
	}

	result, err := service.TestBoost(ctx, boost, triggerData)
	require.NoError(t, err)
	require.Len(t, result.ConditionResults, 1)
	assert.Equal(t, "Food And Drink", result.ConditionResults[0].ValueDisplay)
	assert.Equal(t, "Food And Drink", result.ConditionResults[0].ActualDisplay)
}

// TestBoostTestExecution_CategoryScopedActualPopulated verifies that TestBoost sets Actual to the
// category metric value from the nested categories map for budget_category.* conditions (A-CR9).
func TestBoostTestExecution_CategoryScopedActualPopulated(t *testing.T) {
	ctx := context.Background()

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	boost := &domain.Boost{
		ID:   "rule-cat-actual",
		Name: "Category Actual Test",
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				Operator:   domain.ConditionOperatorGte,
				Value:      "50",
				BudgetID:   "B1",
				CategoryID: "C1",
			},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{}`),
	}

	t.Run("interface shape (River JSON round-trip)", func(t *testing.T) {
		triggerData := map[string]interface{}{
			"budget_id": "B1",
			"categories": map[string]interface{}{
				"C1": map[string]interface{}{"percent_used": 85.0},
			},
		}
		result, err := service.TestBoost(ctx, boost, triggerData)
		require.NoError(t, err)
		require.Len(t, result.ConditionResults, 1)
		assert.Equal(t, float64(85), result.ConditionResults[0].Actual)
		assert.True(t, result.ConditionResults[0].Met)
	})

	t.Run("typed map shape (in-process path)", func(t *testing.T) {
		triggerData := map[string]interface{}{
			"budget_id":  "B1",
			"categories": map[string]map[string]float64{"C1": {"percent_used": 85.0}},
		}
		result, err := service.TestBoost(ctx, boost, triggerData)
		require.NoError(t, err)
		require.Len(t, result.ConditionResults, 1)
		assert.Equal(t, float64(85), result.ConditionResults[0].Actual)
		assert.True(t, result.ConditionResults[0].Met)
	})
}

// TestBoostTestExecution_CategoryActualNilForMissingCategory verifies that TestBoost sets Actual=nil
// when the category is absent from event data (A-CR9: missing/deleted category → nil).
func TestBoostTestExecution_CategoryActualNilForMissingCategory(t *testing.T) {
	ctx := context.Background()

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	boost := &domain.Boost{
		ID:   "rule-cat-nil",
		Name: "Category Nil Test",
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				Operator:   domain.ConditionOperatorGte,
				Value:      "50",
				BudgetID:   "B1",
				CategoryID: "C1",
			},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{}`),
	}

	t.Run("empty categories map", func(t *testing.T) {
		triggerData := map[string]interface{}{
			"budget_id":  "B1",
			"categories": map[string]interface{}{},
		}
		result, err := service.TestBoost(ctx, boost, triggerData)
		require.NoError(t, err)
		require.Len(t, result.ConditionResults, 1)
		assert.Nil(t, result.ConditionResults[0].Actual)
		assert.False(t, result.ConditionResults[0].Met)
	})

	t.Run("categories key absent", func(t *testing.T) {
		triggerData := map[string]interface{}{"budget_id": "B1"}
		result, err := service.TestBoost(ctx, boost, triggerData)
		require.NoError(t, err)
		require.Len(t, result.ConditionResults, 1)
		assert.Nil(t, result.ConditionResults[0].Actual)
		assert.False(t, result.ConditionResults[0].Met)
	})
}

// TestBoostExecutionService_ExpiresAt verifies that suggestions created by
// executeTransferSuggestion and executeSavingsGoal have ExpiresAt set ~30 days
// in the future.
func TestBoostExecutionService_ExpiresAt(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-expires"

	t.Run("transfer suggestion has ExpiresAt ~30 days in future", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

		actionsJSON, _ := json.Marshal(domain.TransferAction{
			FromAccountID: "account-from",
			ToAccountID:   "account-to",
			AmountType:    "fixed",
			Amount:        100.0,
		})

		boost := &domain.Boost{
			ID:        "rule-expires-test",
			UserID:    userID,
			Name:      "Transfer Rule",
			Enabled:   true,
			Priority:  1,
			BoostType: domain.BoostTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions:     []domain.BoostCondition{},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}

		before := time.Now().UTC()
		_, err := service.ExecuteBoost(ctx, boost, map[string]interface{}{"amount": 100.0})
		after := time.Now().UTC()

		require.NoError(t, err)
		require.Len(t, suggestionRepo.suggestions, 1)

		suggestion := suggestionRepo.suggestions[0]
		require.NotNil(t, suggestion.ExpiresAt, "ExpiresAt must be set for transfer suggestions")

		expectedLow := before.Add(29 * 24 * time.Hour)
		expectedHigh := after.Add(31 * 24 * time.Hour)
		assert.True(t, suggestion.ExpiresAt.After(expectedLow),
			"ExpiresAt %v should be after %v", *suggestion.ExpiresAt, expectedLow)
		assert.True(t, suggestion.ExpiresAt.Before(expectedHigh),
			"ExpiresAt %v should be before %v", *suggestion.ExpiresAt, expectedHigh)
	})

	t.Run("savings goal suggestion has ExpiresAt ~30 days in future", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}

		// Build a mock savings goal repo that returns a goal.
		mockGoalRepo := &mockSavingsGoalRepository{}
		goalID := "goal-1"
		accountID := "savings-account"
		mockGoalRepo.goals = []*domain.SavingsGoal{
			{
				ID:        goalID,
				Name:      "Vacation Fund",
				AccountID: accountID,
				UserID:    userID,
			},
		}

		service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)
		service.SetSavingsGoalRepo(mockGoalRepo)

		actionsJSON, _ := json.Marshal(domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: "checking-account",
			AmountType:    "fixed",
			Amount:        50.0,
		})

		boost := &domain.Boost{
			ID:        "rule-savings-expires",
			UserID:    userID,
			Name:      "Savings Goal Rule",
			Enabled:   true,
			Priority:  1,
			BoostType: domain.BoostTypeSavingsGoal,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions:     []domain.BoostCondition{},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}

		before := time.Now().UTC()
		_, err := service.ExecuteBoost(ctx, boost, map[string]interface{}{"amount": 50.0})
		after := time.Now().UTC()

		require.NoError(t, err)
		require.Len(t, suggestionRepo.suggestions, 1)

		suggestion := suggestionRepo.suggestions[0]
		require.NotNil(t, suggestion.ExpiresAt, "ExpiresAt must be set for savings goal suggestions")

		expectedLow := before.Add(29 * 24 * time.Hour)
		expectedHigh := after.Add(31 * 24 * time.Hour)
		assert.True(t, suggestion.ExpiresAt.After(expectedLow),
			"ExpiresAt %v should be after %v", *suggestion.ExpiresAt, expectedLow)
		assert.True(t, suggestion.ExpiresAt.Before(expectedHigh),
			"ExpiresAt %v should be before %v", *suggestion.ExpiresAt, expectedHigh)
	})
}

// TestEvaluateCondition_DottedFieldNormalization verifies that evaluateCondition maps
// UI-built dotted field names (e.g. "budget.remaining_amount") to the flat engine keys
// (e.g. "remaining") before doing the data lookup. Without the normalization shim,
// every UI-built condition silently returns false because the engine emits flat keys.
// Issue #972: GAP M – normalization shim.
func TestEvaluateCondition_DottedFieldNormalization(t *testing.T) {
	tests := []struct {
		name       string
		field      string
		categoryID string // non-empty only for budget_category.* tests (Issue #1270)
		operator   domain.ConditionOperator
		value      string
		data       map[string]interface{}
		wantMatch  bool
	}{
		{
			name:      "budget.remaining_amount maps to remaining (gt, matches)",
			field:     "budget.remaining_amount",
			operator:  domain.ConditionOperatorGt,
			value:     "100",
			data:      map[string]interface{}{"remaining": 150.0},
			wantMatch: true,
		},
		{
			name:      "budget.remaining_amount maps to remaining (gt, no match)",
			field:     "budget.remaining_amount",
			operator:  domain.ConditionOperatorGt,
			value:     "200",
			data:      map[string]interface{}{"remaining": 150.0},
			wantMatch: false,
		},
		{
			name:      "budget.total_spent maps to total_spent",
			field:     "budget.total_spent",
			operator:  domain.ConditionOperatorGte,
			value:     "500",
			data:      map[string]interface{}{"total_spent": 500.0},
			wantMatch: true,
		},
		{
			name:      "budget.percent_used maps to percent_used",
			field:     "budget.percent_used",
			operator:  domain.ConditionOperatorGt,
			value:     "75",
			data:      map[string]interface{}{"percent_used": 82.5},
			wantMatch: true,
		},
		{
			// Issue #1270: budget_category.* uses nested categories[CategoryID] lookup.
			name:       "budget_category.remaining_amount uses nested category lookup",
			field:      "budget_category.remaining_amount",
			categoryID: "cat-abc",
			operator:   domain.ConditionOperatorLt,
			value:      "50",
			data: map[string]interface{}{
				domain.EventDataKeyCategories: map[string]interface{}{
					"cat-abc": map[string]interface{}{"remaining": 30.0},
				},
			},
			wantMatch: true,
		},
		{
			// Issue #1270: spent_amount suffix alias → "spent" key in category map.
			// Issue #1271: key renamed from "total_spent" to "spent".
			name:       "budget_category.spent_amount aliases to spent in category map",
			field:      "budget_category.spent_amount",
			categoryID: "cat-abc",
			operator:   domain.ConditionOperatorGt,
			value:      "100",
			data: map[string]interface{}{
				domain.EventDataKeyCategories: map[string]interface{}{
					"cat-abc": map[string]interface{}{"spent": 200.0},
				},
			},
			wantMatch: true,
		},
		{
			// Issue #1270: budget_category.percent_used reads from per-category data (no
			// longer falls through to flat budget-level percent_used).
			name:       "budget_category.percent_used reads per-category data",
			field:      "budget_category.percent_used",
			categoryID: "cat-abc",
			operator:   domain.ConditionOperatorGt,
			value:      "50",
			data: map[string]interface{}{
				domain.EventDataKeyCategories: map[string]interface{}{
					"cat-abc": map[string]interface{}{"percent_used": 80.0},
				},
			},
			wantMatch: true,
		},
		{
			// Regression: budget_category.* without a matching CategoryID returns false
			// (no category found → no match).
			name:       "budget_category.* with wrong category ID returns false",
			field:      "budget_category.remaining_amount",
			categoryID: "cat-other",
			operator:   domain.ConditionOperatorLt,
			value:      "50",
			data: map[string]interface{}{
				domain.EventDataKeyCategories: map[string]interface{}{
					"cat-abc": map[string]interface{}{"remaining": 30.0},
				},
			},
			wantMatch: false,
		},
		{
			name:      "account.balance maps to balance",
			field:     "account.balance",
			operator:  domain.ConditionOperatorGte,
			value:     "1000",
			data:      map[string]interface{}{"balance": 2500.0},
			wantMatch: true,
		},
		{
			name:      "transaction.amount maps to amount",
			field:     "transaction.amount",
			operator:  domain.ConditionOperatorGt,
			value:     "50",
			data:      map[string]interface{}{"amount": 75.0},
			wantMatch: true,
		},
		{
			// Regression: flat keys must still work (no double-mapping).
			name:      "flat key remaining still works (backward compat)",
			field:     "remaining",
			operator:  domain.ConditionOperatorGt,
			value:     "100",
			data:      map[string]interface{}{"remaining": 150.0},
			wantMatch: true,
		},
		{
			// Regression: flat key percent_used must still work.
			name:      "flat key percent_used still works (backward compat)",
			field:     "percent_used",
			operator:  domain.ConditionOperatorGte,
			value:     "75",
			data:      map[string]interface{}{"percent_used": 82.5},
			wantMatch: true,
		},
		{
			// Unknown field should still return false (key absent from data).
			name:      "unknown dotted field returns false",
			field:     "foo.bar",
			operator:  domain.ConditionOperatorEq,
			value:     "x",
			data:      map[string]interface{}{"baz": "x"},
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			condition := domain.BoostCondition{
				Field:      tc.field,
				CategoryID: tc.categoryID,
				Operator:   tc.operator,
				Value:      tc.value,
			}
			got := evaluateCondition(condition, tc.data)
			assert.Equal(t, tc.wantMatch, got)
		})
	}
}

// TestEventDataKeyContract asserts that every key the calculators read from EventData
// is present in the map emitted by the period-close worker. This is a drift guard —
// if either the emitter or the readers are updated and the constants diverge, this
// test fails. Issue #972: GAP J – emitter/reader parity.
// Issue #1270: EventDataKeyCategories added to emitted set.
func TestEventDataKeyContract(t *testing.T) {
	// Keys emitted by the period-close worker (budget_period_close_check.go).
	emittedKeys := map[string]bool{
		domain.EventDataKeyBudgetID:     true,
		domain.EventDataKeyRemaining:    true,
		domain.EventDataKeyIncomeAmount: true,
		domain.EventDataKeyAmount:       true,
		domain.EventDataKeyTotalSpent:   true,
		domain.EventDataKeyPercentUsed:  true, // Issue #972 A-min: emitted via Budget.PercentUsed()
		domain.EventDataKeyPeriodStart:  true,
		domain.EventDataKeyPeriodEnd:    true,
		domain.EventDataKeyCategories:   true, // Issue #1270: per-category data map
	}

	// Keys that calculateTransferAmount reads.
	transferReaderKeys := []string{
		domain.EventDataKeyAmount,    // "percentage" and "threshold" branches
		domain.EventDataKeyRemaining, // "remaining" branch
	}

	// Keys that calculateSavingsGoalAmount reads.
	savingsReaderKeys := []string{
		domain.EventDataKeyAmount,    // "percentage" branch
		domain.EventDataKeyRemaining, // "remaining" branch
	}

	for _, key := range transferReaderKeys {
		assert.True(t, emittedKeys[key],
			"calculateTransferAmount reads key %q which is NOT in the period-close emitted map", key)
	}

	for _, key := range savingsReaderKeys {
		assert.True(t, emittedKeys[key],
			"calculateSavingsGoalAmount reads key %q which is NOT in the period-close emitted map", key)
	}
}

// TestBudgetPercentUsedConditionFires verifies Issue #972 Work Item A-min:
// a rule with condition {Field: "budget.percent_used", Operator: gt, Value: "80"}
// evaluates to TRUE when the period-close emitter supplies percent_used > 80, and
// FALSE for a zero-Amount budget (PercentUsed()=0, no divide-by-zero).
// This is a contract test: it locks the three-layer agreement between
//  1. the UI offering "budget.percent_used" as a condition field,
//  2. the emitter publishing domain.EventDataKeyPercentUsed via Budget.PercentUsed(),
//  3. evaluateCondition normalizing "budget.percent_used" → "percent_used".
func TestBudgetPercentUsedConditionFires(t *testing.T) {
	t.Run("fires when percent used exceeds threshold", func(t *testing.T) {
		// EventData as the period-close worker now emits: Budget.PercentUsed() = 82%.
		eventData := map[string]interface{}{
			domain.EventDataKeyPercentUsed: 82.5, // 82.5% used
			domain.EventDataKeyRemaining:   250.0,
			domain.EventDataKeyAmount:      1000.0,
		}
		condition := domain.BoostCondition{
			Field:    "budget.percent_used",
			Operator: domain.ConditionOperatorGt,
			Value:    "80",
		}
		assert.True(t, evaluateCondition(condition, eventData),
			"budget.percent_used > 80 should fire when PercentUsed()=82.5")
	})

	t.Run("does not fire when percent used is below threshold", func(t *testing.T) {
		eventData := map[string]interface{}{
			domain.EventDataKeyPercentUsed: 50.0,
		}
		condition := domain.BoostCondition{
			Field:    "budget.percent_used",
			Operator: domain.ConditionOperatorGt,
			Value:    "80",
		}
		assert.False(t, evaluateCondition(condition, eventData),
			"budget.percent_used > 80 should not fire when PercentUsed()=50")
	})

	t.Run("zero-Amount budget emits 0 (no divide-by-zero)", func(t *testing.T) {
		// Budget.PercentUsed() returns 0 when Amount==0 and TotalSpent()==0.
		budget := &domain.Budget{ID: "b0", Amount: 0}
		assert.Equal(t, 0.0, budget.PercentUsed(),
			"PercentUsed() must return 0 for a zero-Amount budget with no spending")

		eventData := map[string]interface{}{
			domain.EventDataKeyPercentUsed: budget.PercentUsed(), // 0.0
		}
		condition := domain.BoostCondition{
			Field:    "budget.percent_used",
			Operator: domain.ConditionOperatorGt,
			Value:    "80",
		}
		assert.False(t, evaluateCondition(condition, eventData),
			"budget.percent_used > 80 must be false for a zero-Amount budget")
	})
}

// TestBoostTestExecution_DottedFieldPreviewShowsActual verifies GAP T: TestBoost's preview must
// expose the real EventData value for dotted UI-built condition fields (e.g.
// "budget.remaining_amount") in ConditionEvalResult.Actual, not nil.
// Without normalizeConditionField applied to the Actual lookup, dotted fields miss in
// the EventData map and Actual is set to nil even though Met is computed correctly.
func TestBoostTestExecution_DottedFieldPreviewShowsActual(t *testing.T) {
	ctx := context.Background()
	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	actions, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "account-a", ToAccountID: "account-b",
		AmountType: "fixed", Amount: 50,
	})
	boost := &domain.Boost{
		ID: "rule-dotted", UserID: "u", Name: "dotted", Enabled: true, Priority: 1,
		BoostType:     domain.BoostTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
		// UI-built dotted field — backend emits flat "remaining"
		Conditions: []domain.BoostCondition{
			{Field: "budget.remaining_amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		},
		ConditionLogic: "AND",
		Actions:        actions,
	}
	// Period-close EventData uses flat keys (domain.EventDataKey*)
	triggerData := map[string]interface{}{
		domain.EventDataKeyRemaining: 150.0,
		domain.EventDataKeyBudgetID:  "b-1",
	}

	result, err := service.TestBoost(ctx, boost, triggerData)
	require.NoError(t, err)

	require.Len(t, result.ConditionResults, 1)
	cr := result.ConditionResults[0]
	assert.True(t, cr.Met, "budget.remaining_amount (150) > 100 should be Met")
	// Actual must be the resolved flat value, not nil
	assert.NotNil(t, cr.Actual, "Actual must not be nil for dotted field with matching EventData")
	assert.Equal(t, 150.0, cr.Actual, "Actual must equal the EventData value for budget.remaining_amount")
}

// TestFlatActionJSON_ValidateAndExecuteConsistency proves that rules created with the flat
// action JSON the UI submits pass BOTH domain.Validate() AND the executor, preventing the
// flat/wrapped convention from diverging per rule type.
// Issue #972: GAP W fix — extended to cover all four rule types (savings_goal, transfer,
// budget_alert, category_rollover).
func TestFlatActionJSON_ValidateAndExecuteConsistency(t *testing.T) {
	ctx := context.Background()
	userID := "user-consistency-test"
	goalID := "goal-consistency"

	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: userID, Name: "Emergency Fund", AccountID: "savings-acct"},
		},
	}

	tests := []struct {
		name            string
		boostType       domain.BoostType
		actions         interface{}
		wantSuggestions int
		setupSvc        func(svc *boostExecutionService)
	}{
		{
			name:      "savings_goal flat JSON",
			boostType: domain.BoostTypeSavingsGoal,
			actions: domain.SavingsGoalAction{
				SavingsGoalID: goalID,
				FromAccountID: "checking-acct",
				AmountType:    "fixed",
				Amount:        100.0,
			},
			wantSuggestions: 1,
			setupSvc:        func(svc *boostExecutionService) { svc.SetSavingsGoalRepo(goalRepo) },
		},
		{
			name:      "transfer_suggestion flat JSON (fixed)",
			boostType: domain.BoostTypeTransferSuggestion,
			actions: domain.TransferAction{
				FromAccountID: "checking-1",
				ToAccountID:   "savings-1",
				AmountType:    "fixed",
				Amount:        50.0,
			},
			wantSuggestions: 1,
			setupSvc:        nil,
		},
		{
			// Issue #972 GAP AC: excess is the UI's primary transfer type — amount=0, threshold drives amount.
			// triggerData contains "remaining": 200.0 (set below), threshold=100 → expect suggestion amount 100.
			name:      "transfer_suggestion flat JSON (excess)",
			boostType: domain.BoostTypeTransferSuggestion,
			actions: domain.TransferAction{
				FromAccountID: "checking-1",
				ToAccountID:   "savings-1",
				AmountType:    "excess",
				Amount:        0,
				Threshold:     100.0,
			},
			wantSuggestions: 1,
			setupSvc:        nil,
		},
		{
			// Issue #972 GAP AC (Codex Q4/Q5): legacy 'threshold' type must still validate AND execute
			// so that any saved rules with this type continue to fire after the allow-set was added.
			// triggerData "amount"=200.0, threshold=50 → expect 150.
			name:      "transfer_suggestion flat JSON (threshold legacy)",
			boostType: domain.BoostTypeTransferSuggestion,
			actions: domain.TransferAction{
				FromAccountID: "checking-1",
				ToAccountID:   "savings-1",
				AmountType:    "threshold",
				Threshold:     50.0,
			},
			wantSuggestions: 1,
			setupSvc:        nil,
		},
		{
			name:      "budget_alert flat JSON",
			boostType: domain.BoostTypeBudgetAlert,
			actions: domain.AlertAction{
				NotificationChannels: []string{"in_app"},
				ThresholdType:        "percentage",
				ThresholdValue:       80.0,
			},
			wantSuggestions: 0, // budget alert has no suggestion side-effect
			setupSvc:        nil,
		},
		{
			name:      "category_rollover flat JSON",
			boostType: domain.BoostTypeCategoryRollover,
			actions: domain.RolloverAction{
				SourceBudgetCategoryID: "cat-1",
				RolloverType:           "full",
			},
			wantSuggestions: 1,
			setupSvc:        nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actionsJSON, _ := json.Marshal(tc.actions)
			boost := &domain.Boost{
				ID:             "rule-flat-" + tc.name,
				UserID:         userID,
				Name:           "Flat Action Rule",
				Enabled:        true,
				BoostType:      tc.boostType,
				TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
				ConditionLogic: "AND",
				Conditions:     []domain.BoostCondition{},
				Actions:        actionsJSON,
			}

			// Validation must pass with flat JSON.
			require.NoError(t, boost.Validate(), "flat action JSON must pass domain validation for %s", tc.name)

			// Execution must also succeed with the same flat JSON.
			execRepo := &mockBoostExecutionRepository{}
			suggestionRepo := &mockTransferSuggestionRepository{}
			boostRepo := &mockBoostsRepository{}
			svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)
			if tc.setupSvc != nil {
				tc.setupSvc(svc)
			}

			// Use both period-close keys so all amount-type branches (including legacy "threshold") have data.
			_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{"remaining": 200.0, "amount": 200.0})
			require.NoError(t, err, "flat action JSON must execute successfully (%s)", tc.name)
			assert.Len(t, suggestionRepo.suggestions, tc.wantSuggestions,
				"suggestion count for %s", tc.name)
		})
	}
}

// TestExecuteBoost_PartialFieldAction_ReturnsError proves that ExecuteBoost validates the boost
// before dispatching, so a partial/malformed action is rejected before creating side-effects.
// Issue #972: GAP Y defense-in-depth.
// RED: fails today (executors gate only one field; partial actions proceed past single-field guard).
// GREEN: add boost.Validate() at top of ExecuteBoost.
func TestExecuteBoost_PartialFieldAction_ReturnsError(t *testing.T) {
	ctx := context.Background()
	userID := "user-partial-test"

	tests := []struct {
		name            string
		boostType       domain.BoostType
		actions         interface{} // intentionally incomplete
		wantErrContains string
	}{
		{
			name:      "transfer missing to_account_id",
			boostType: domain.BoostTypeTransferSuggestion,
			actions: domain.TransferAction{
				FromAccountID: "acct-1",
				// ToAccountID intentionally absent
				AmountType: "fixed",
				Amount:     50.0,
			},
			wantErrContains: "to_account_id",
		},
		{
			name:      "savings_goal missing from_account_id",
			boostType: domain.BoostTypeSavingsGoal,
			actions: domain.SavingsGoalAction{
				SavingsGoalID: "goal-1",
				// FromAccountID intentionally absent
				AmountType: "fixed",
				Amount:     50.0,
			},
			wantErrContains: "from_account_id",
		},
		{
			name:      "budget_alert missing notification_channels",
			boostType: domain.BoostTypeBudgetAlert,
			actions: domain.AlertAction{
				// NotificationChannels intentionally absent
				ThresholdType:  "percentage",
				ThresholdValue: 80.0,
			},
			wantErrContains: "notification_channels",
		},
		{
			name:      "category_rollover missing source_budget_category_id",
			boostType: domain.BoostTypeCategoryRollover,
			actions: domain.RolloverAction{
				// SourceBudgetCategoryID intentionally absent
				RolloverType: "full",
			},
			wantErrContains: "source_budget_category_id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actionsJSON, _ := json.Marshal(tc.actions)
			boost := &domain.Boost{
				ID:             "rule-partial-" + tc.name,
				UserID:         userID,
				Name:           "Partial Boost",
				Enabled:        true,
				BoostType:      tc.boostType,
				TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
				ConditionLogic: "AND",
				Conditions:     []domain.BoostCondition{},
				Actions:        actionsJSON,
			}

			execRepo := &mockBoostExecutionRepository{}
			suggestionRepo := &mockTransferSuggestionRepository{}
			boostRepo := &mockBoostsRepository{}
			svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

			_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
			require.Error(t, err, "partial action must be rejected by ExecuteBoost")
			assert.Contains(t, err.Error(), tc.wantErrContains,
				"error must mention the missing field for %s", tc.name)
			// No suggestion should be created — the action was rejected before side-effects.
			assert.Empty(t, suggestionRepo.suggestions,
				"no suggestion must be created for partial action (%s)", tc.name)
		})
	}
}

// TestTransferExcess_ValidateAndExecute validates the 'excess' amount_type:
// - domain.Validate() accepts amount=0 for excess rules
// - executeTransferSuggestion computes amount = remaining - threshold
// - No suggestion created when remaining <= threshold
func TestTransferExcess_ValidateAndExecute(t *testing.T) {
	ctx := context.Background()
	userID := "user-excess-test"

	excessAction := domain.TransferAction{
		FromAccountID: "checking-1",
		ToAccountID:   "savings-1",
		AmountType:    "excess",
		Amount:        0,
		Threshold:     50.0,
	}
	actionsJSON, err := json.Marshal(excessAction)
	require.NoError(t, err)

	makeBoost := func() *domain.Boost {
		return &domain.Boost{
			ID:             "rule-excess-1",
			UserID:         userID,
			Name:           "Excess Savings",
			Enabled:        true,
			BoostType:      domain.BoostTypeTransferSuggestion,
			TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
			ConditionLogic: "AND",
			Conditions:     []domain.BoostCondition{},
			Actions:        actionsJSON,
		}
	}

	t.Run("domain.Validate accepts excess boost with amount=0 and threshold", func(t *testing.T) {
		require.NoError(t, makeBoost().Validate(),
			"excess transfer boost (amount=0, threshold>0) must pass domain.Validate()")
	})

	t.Run("excess creates suggestion for remaining - threshold when remaining > threshold", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

		triggerData := map[string]interface{}{
			domain.EventDataKeyRemaining: 200.0, // remaining=200, threshold=50 → expect 150
		}
		_, err := svc.ExecuteBoost(ctx, makeBoost(), triggerData)
		require.NoError(t, err, "excess rule must execute without error")
		require.Len(t, suggestionRepo.suggestions, 1, "excess rule must create one suggestion")
		assert.InDelta(t, 150.0, suggestionRepo.suggestions[0].Amount, 0.001,
			"suggestion amount must be remaining(200) - threshold(50) = 150")
	})

	t.Run("excess creates no suggestion when remaining <= threshold", func(t *testing.T) {
		execRepo := &mockBoostExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		boostRepo := &mockBoostsRepository{}
		svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

		triggerData := map[string]interface{}{
			domain.EventDataKeyRemaining: 30.0, // remaining=30, threshold=50 → no surplus
		}
		_, err := svc.ExecuteBoost(ctx, makeBoost(), triggerData)
		// When amount≤0, executeTransferSuggestion should either not create a suggestion
		// or return early. Either way, no suggestion is expected.
		if err == nil {
			assert.Empty(t, suggestionRepo.suggestions,
				"excess rule with remaining<=threshold must not create a suggestion")
		}
	})
}

// ============================================================================
// Issue #972 GAP AE — execution-time IDOR defense (savings_goal)
// ============================================================================

// TestExecuteSavingsGoal_RejectsMismatchedOwner verifies that executeSavingsGoal skips
// (returns nil, creates no suggestion) when the referenced savings_goal_id belongs to a
// different user than rule.UserID. Uses GetByIDForUser which scopes by user_id.
func TestExecuteSavingsGoal_RejectsMismatchedOwner(t *testing.T) {
	ctx := context.Background()

	ownerUserID := "user-owner"
	attackerUserID := "user-attacker"
	goalID := "goal-owned-by-owner"

	// Goal belongs to ownerUserID, not attackerUserID.
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{
				ID:        goalID,
				UserID:    ownerUserID,
				Name:      "Owner's Goal",
				AccountID: "savings-acct-owner",
			},
		},
	}
	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)
	svc.SetSavingsGoalRepo(goalRepo)

	actionsJSON, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": goalID,
		"from_account_id": "from-attacker-acct",
		"amount_type":     "fixed",
		"amount":          100.0,
	})
	boost := &domain.Boost{
		ID:        "attacker-rule-id",
		UserID:    attackerUserID, // ← attacker's boost, references owner's goal
		Name:      "attacker boost",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        actionsJSON,
		ConditionLogic: "AND",
	}
	triggerData := map[string]interface{}{
		domain.EventDataKeyRemaining: 500.0,
		domain.EventDataKeyAmount:    1000.0,
	}

	_, err := svc.ExecuteBoost(ctx, boost, triggerData)
	// The execution should NOT error (skip+log, not fail the batch), but must NOT create a suggestion.
	require.NoError(t, err, "mismatched-owner savings_goal boost must be skipped (not error)")
	assert.Empty(t, suggestionRepo.suggestions,
		"no suggestion must be created for a savings_goal boost referencing another user's goal")
}

// TestExecuteSavingsGoal_AmbiguousChain_UsesPhysicalAccount_NotGuess pins #1569 R11 T2's
// deliberate site-3 decision: when the savings goal's chain has more than one live account
// (ErrAmbiguousActiveAccount), executeSavingsGoal falls back to the goal's own physical
// AccountID rather than guessing between the two live siblings — and, UNLIKE site 2 (the
// transfer-suggestion source/destination), it still creates the suggestion, because the
// physical row IS the account the goal was created against, not an unrelated guess.
func TestExecuteSavingsGoal_AmbiguousChain_UsesPhysicalAccount_NotGuess(t *testing.T) {
	ctx := context.Background()

	userID := "user-1"
	goalID := "goal-ambiguous-chain"
	uaid := "uaid-ambiguous"

	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{
				ID:            goalID,
				UserID:        userID,
				Name:          "Emergency Fund",
				AccountID:     "physical-savings-account",
				UserAccountID: &uaid,
			},
		},
	}
	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetChainResolver(&stubChainResolver{resolveErr: ErrAmbiguousActiveAccount})

	actionsJSON, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": goalID,
		"from_account_id": "from-account",
		"amount_type":     "fixed",
		"amount":          100.0,
	})
	boost := &domain.Boost{
		ID:        "rule-savings-ambiguous",
		UserID:    userID,
		Name:      "Ambiguous chain savings boost",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        actionsJSON,
		ConditionLogic: "AND",
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{
		domain.EventDataKeyRemaining: 500.0,
		domain.EventDataKeyAmount:    1000.0,
	})
	require.NoError(t, err)

	require.Len(t, suggestionRepo.suggestions, 1,
		"an ambiguous chain must not block the savings-goal suggestion — the physical account is a safe fallback")
	require.NotNil(t, suggestionRepo.suggestions[0].ToAccountID)
	assert.Equal(t, "physical-savings-account", *suggestionRepo.suggestions[0].ToAccountID,
		"must use the goal's own physical account, not guess between the two live siblings")
}

// ============================================================================
// GAP AF — derived transfer types must return 0 when trigger key absent
// ============================================================================

// TestCalculateTransferAmount_PercentageUsesIncome verifies that the "percentage" amount_type
// computes against period INCOME (EventDataKeyAmount), NOT the surplus/remaining.
// This is the semantic-lock test for GAP AD: the UI label was updated from "percentage of
// the balance" to "percentage of period income" to match this behavior. Reverting the executor
// to read EventDataKeyRemaining instead of EventDataKeyAmount must make this test fail.
//
// Setup: income=$1000, remaining=$200, percentage=10% → expected $100 (10% of income).
// If the executor incorrectly read "remaining": $200 × 10% = $20 ≠ $100.
func TestCalculateTransferAmount_PercentageUsesIncome(t *testing.T) {
	svc := &boostExecutionService{}

	action := domain.TransferAction{
		FromAccountID: "from-a",
		ToAccountID:   "to-a",
		AmountType:    "percentage",
		Amount:        10.0, // 10%
	}

	triggerData := map[string]interface{}{
		domain.EventDataKeyAmount:    1000.0, // income (the base the percentage is applied to)
		domain.EventDataKeyRemaining: 200.0,  // surplus — must NOT be used for percentage
	}

	result := svc.calculateTransferAmount(&action, triggerData)

	assert.InDelta(t, 100.0, result, 0.001,
		"percentage must be 10%% of INCOME ($1000), not of remaining ($200); got %v", result)
}

// TestCalculateSavingsGoalAmount_PercentageUsesIncome verifies the same income-vs-surplus
// semantic for the savings_goal calculator (both calculators must agree on the base).
func TestCalculateSavingsGoalAmount_PercentageUsesIncome(t *testing.T) {
	svc := &boostExecutionService{}

	pct := float64(10)
	action := domain.SavingsGoalAction{
		SavingsGoalID: "goal-1",
		FromAccountID: "from-a",
		AmountType:    "percentage",
		Percentage:    &pct, // 10%
	}

	triggerData := map[string]interface{}{
		domain.EventDataKeyAmount:    1000.0, // income (the base)
		domain.EventDataKeyRemaining: 200.0,  // surplus — must NOT be used for percentage
	}

	got := svc.calculateSavingsGoalAmount(&action, triggerData)
	assert.InDelta(t, 100.0, got, 0.001,
		"savings_goal percentage must be 10%% of INCOME ($1000), not of remaining ($200); got %v", got)
}

// ============================================================================
// GAP AE — chain-resolver IDOR: execution-time ownership guard
// ============================================================================

// stubChainResolver is an inline AccountChainResolver that returns a fixed resolved ID.
type stubChainResolver struct {
	resolvedID string
	resolveErr error
}

func (s *stubChainResolver) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	return nil, nil
}
func (s *stubChainResolver) ResolveActiveID(_ context.Context, _, _ string) (string, error) {
	return s.resolvedID, s.resolveErr
}
func (s *stubChainResolver) ChainAccountIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// TestExecuteTransferSuggestion_ChainResolverIDOR verifies that executeTransferSuggestion
// skips (returns nil, no suggestion) when the chain resolver resolves a from_user_account_id
// to an account that belongs to a DIFFERENT user than rule.UserID.
//
// Attack scenario: attacker creates a rule with their own from_account_id (passes the
// GetByIDAndUser ownership check) plus a victim's from_user_account_id. At execution time,
// the unscoped ResolveActiveID returns the victim's current account.id. Without this guard
// the suggestion would be created with the victim's account in FromAccountID (information
// leakage / IDOR). With the guard, the executor detects the ownership mismatch and skips.
func TestExecuteTransferSuggestion_ChainResolverIDOR(t *testing.T) {
	ctx := context.Background()

	attackerUserID := "user-attacker"
	victimAccountID := "victim-active-account-id" // what the unscoped resolver returns

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}

	// The standard from_account_id and to_account_id both belong to the attacker.
	acctRepo.On("GetByIDAndUser", mock.Anything, "from-attacker", attackerUserID).
		Return(&domain.Account{ID: "from-attacker"}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, "to-attacker", attackerUserID).
		Return(&domain.Account{ID: "to-attacker"}, nil)
	// The chain-resolved victim's account does NOT belong to the attacker.
	acctRepo.On("GetByIDAndUser", mock.Anything, victimAccountID, attackerUserID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)
	svc.SetAccountRepo(acctRepo)
	// Chain resolver returns the victim's account ID when asked to resolve the from_user_account_id.
	svc.SetChainResolver(&stubChainResolver{resolvedID: victimAccountID})

	actionsJSON, _ := json.Marshal(map[string]interface{}{
		"from_account_id":      "from-attacker",
		"to_account_id":        "to-attacker",
		"from_user_account_id": "ua-victim-stable-ref", // victim's stable user_account_id
		"amount_type":          "fixed",
		"amount":               100.0,
	})
	boost := &domain.Boost{
		ID:        "attacker-rule",
		UserID:    attackerUserID,
		Name:      "chain resolver idor boost",
		Enabled:   true,
		BoostType: domain.BoostTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        actionsJSON,
		ConditionLogic: "AND",
	}
	triggerData := map[string]interface{}{
		domain.EventDataKeyRemaining: 500.0,
		domain.EventDataKeyAmount:    1000.0,
	}

	_, err := svc.ExecuteBoost(ctx, boost, triggerData)
	require.NoError(t, err,
		"chain-resolver IDOR must cause a skip (nil return), not an error that fails the batch")
	assert.Empty(t, suggestionRepo.suggestions,
		"no suggestion must be created when the chain-resolved account belongs to a different user")
}

// TestCalculateTransferAmount_DerivedTypesReturnZeroOnMissingKey verifies that transfer
// amount_types that derive their value from EventData ("remaining", "excess", "threshold")
// return 0 — NOT the literal action.Amount — when the required EventData key is absent
// (e.g. schedule triggers, or a future emitter that omits the key).
//
// Background: calculateTransferAmount's final `return action.Amount` is intended only for
// the "fixed" case (and unknown types). Derived types MUST return 0 when their key is
// missing to avoid creating phantom suggestions with whatever default amount was set in the
// RuleBuilder (typically $100). GAP AF: before this fix, "excess"/"remaining"/"threshold"
// cases fell through to `return action.Amount` when the key was absent.
func TestCalculateTransferAmount_DerivedTypesReturnZeroOnMissingKey(t *testing.T) {
	svc := &boostExecutionService{}

	// action.Amount is intentionally set to a non-zero value (the RuleBuilder default)
	// to prove the derived cases do NOT fall back to it.
	baseAction := domain.TransferAction{
		FromAccountID: "from-a",
		ToAccountID:   "to-a",
		Amount:        100.0, // RuleBuilder default — must NOT be used for derived types
		Threshold:     50.0,
	}

	t.Run("remaining returns 0 when EventDataKeyRemaining absent", func(t *testing.T) {
		action := baseAction
		action.AmountType = "remaining"
		result := svc.calculateTransferAmount(&action, map[string]interface{}{
			// EventDataKeyRemaining intentionally omitted (e.g. schedule trigger)
			domain.EventDataKeyAmount: 1000.0,
		})
		assert.Equal(t, 0.0, result,
			"remaining must return 0 when the 'remaining' key is absent, not fall back to action.Amount")
	})

	t.Run("excess returns 0 when EventDataKeyRemaining absent", func(t *testing.T) {
		action := baseAction
		action.AmountType = "excess"
		result := svc.calculateTransferAmount(&action, map[string]interface{}{
			// EventDataKeyRemaining intentionally omitted
			domain.EventDataKeyAmount: 1000.0,
		})
		assert.Equal(t, 0.0, result,
			"excess must return 0 when the 'remaining' key is absent, not fall back to action.Amount ($100)")
	})

	t.Run("threshold returns 0 when EventDataKeyAmount absent", func(t *testing.T) {
		action := baseAction
		action.AmountType = "threshold"
		result := svc.calculateTransferAmount(&action, map[string]interface{}{
			// EventDataKeyAmount intentionally omitted
			domain.EventDataKeyRemaining: 200.0,
		})
		assert.Equal(t, 0.0, result,
			"threshold must return 0 when the 'amount' key is absent, not fall back to action.Amount ($100)")
	})

	t.Run("threshold returns 0 when amount does not exceed threshold (no fallback)", func(t *testing.T) {
		action := baseAction
		action.AmountType = "threshold"
		result := svc.calculateTransferAmount(&action, map[string]interface{}{
			domain.EventDataKeyAmount: 30.0, // Below threshold of 50 → no transfer
		})
		assert.Equal(t, 0.0, result,
			"threshold must return 0 when amount<=threshold, not fall back to action.Amount")
	})

	// C12: percentage missing income key → 0 (not action.Amount fallback).
	// When the budget income key is absent (e.g., schedule trigger or future emitter change),
	// percentage must return 0 rather than computing a percentage of the stale action.Amount.
	t.Run("percentage returns 0 when EventDataKeyAmount (income) absent", func(t *testing.T) {
		action := baseAction
		action.AmountType = "percentage"
		action.Amount = 10.0 // 10% — but income key is missing
		result := svc.calculateTransferAmount(&action, map[string]interface{}{
			// EventDataKeyAmount (income) intentionally omitted; remaining present but unused
			domain.EventDataKeyRemaining: 500.0,
		})
		assert.Equal(t, 0.0, result,
			"percentage must return 0 when the income key is absent, not fall back to action.Amount ($10)")
	})

	t.Run("fixed still uses action.Amount (regression guard)", func(t *testing.T) {
		action := baseAction
		action.AmountType = "fixed"
		result := svc.calculateTransferAmount(&action, map[string]interface{}{})
		assert.Equal(t, 100.0, result, "fixed must return action.Amount regardless of EventData")
	})
}

// TestResolveAndVerifyUserAccount_AccountRepoNilFailOpen documents the intentional
// behavior: when accountRepo is nil (e.g. in unit tests that don't set SetAccountRepo),
// resolveAndVerifyUserAccount applies the resolved ID WITHOUT a DB ownership check and
// logs a warning. This is expected to be dead in production because main.go's
// boostExecutionService wiring block calls SetAccountRepo unconditionally (guarded only
// by db != nil && cfg.ClerkSecretKey != "") and the service-layer create/update
// validation already runs ownership checks — but nothing enforces a non-nil accountRepo,
// so this is an operational wiring assumption, not a guarded invariant.
//
// The test prevents future over-hardening (panicking or returning an error when nil)
// while documenting why the fail-open is intentional. Issue #972 GAP AE + C12.
func TestResolveAndVerifyUserAccount_AccountRepoNilFailOpen(t *testing.T) {
	ctx := context.Background()

	// Build a stub chain resolver that returns a valid resolution.
	stubResolver := &stubChainResolver{resolvedID: "user-account-new"}

	// No accountRepo injected (nil) — simulates a unit test or mis-configured service.
	svc := &boostExecutionService{
		chainResolver: stubResolver,
		accountRepo:   nil, // intentionally nil — fail-open case
	}

	var resolvedID string
	skip, err := svc.resolveAndVerifyUserAccount(ctx, "user-account-old", "user-1", "rule-c12",
		func(id string) { resolvedID = id },
		"from_user_account_id",
	)

	// Must NOT skip the rule or return an error — it should apply the resolved ID.
	require.NoError(t, err, "nil accountRepo must not cause an error")
	assert.False(t, skip, "nil accountRepo must not skip the rule (fail-open)")
	assert.Equal(t, "user-account-new", resolvedID,
		"nil accountRepo must still apply the resolved ID (fail-open; ownership check skipped)")
}

// TestResolveAndVerifyUserAccount_AmbiguousChain_FallsBackLikeNoActiveAccount pins #1569 R11
// T2's site-2 decision (fix round 1: an earlier revision returned skip=true here, which was a
// monotonicity bug — the objectively WORSE condition, ErrNoActiveAccount, already did not skip,
// so skipping only on ambiguity made a stronger-guarantee state produce a WEAKER outcome). The
// caller's pinned id is not an unrelated guess — executeTransferSuggestion already
// ownership-verifies it via accountRepo.GetByIDAndUser before calling this function — so this
// function must NOT skip the rule and must NOT call onResolve, the same as ErrNoActiveAccount.
func TestResolveAndVerifyUserAccount_AmbiguousChain_FallsBackLikeNoActiveAccount(t *testing.T) {
	ctx := context.Background()

	stubResolver := &stubChainResolver{resolveErr: ErrAmbiguousActiveAccount}
	svc := &boostExecutionService{chainResolver: stubResolver}

	onResolveCalled := false
	skip, err := svc.resolveAndVerifyUserAccount(ctx, "user-account-ambiguous", "user-1", "rule-ambiguous",
		func(id string) { onResolveCalled = true },
		"from_user_account_id",
	)

	require.NoError(t, err)
	assert.False(t, skip, "an ambiguous chain must not skip the rule — it must fall back to the pinned id, like ErrNoActiveAccount does")
	assert.False(t, onResolveCalled, "onResolve must not be called for an ambiguous chain — the resolver still refuses to guess WHICH sibling")
}

// ============================================================================
// Issue #1271 Item 2 — A-CR1 brick risk: partial/legacy rule must not brick at execution
// ============================================================================

// TestExecuteBoost_PartialScopedConditionDoesNotBrick verifies that ExecuteBoost does NOT
// return an error when a boost has a budget_category.* condition with empty CategoryID and
// BudgetID. Such boosts may exist as legacy data; they should evaluate to false (no match)
// without bricking (hard error).
func TestExecuteBoost_PartialScopedConditionDoesNotBrick(t *testing.T) {
	ctx := context.Background()

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	// Budget alert boost with a budget_category.percent_used condition that has empty
	// CategoryID and BudgetID — simulating a legacy/partial boost from before #1270.
	actionsJSON, _ := json.Marshal(map[string]interface{}{
		"notification_channels": []string{"email"},
		"threshold_type":        "percentage",
		"threshold_value":       80,
	})
	boost := &domain.Boost{
		ID:        "rule-legacy-partial",
		UserID:    "test-user-id",
		Name:      "Legacy Partial Boost",
		Enabled:   true,
		BoostType: domain.BoostTypeBudgetAlert,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				Operator:   domain.ConditionOperatorGte,
				Value:      "80",
				CategoryID: "", // deliberately empty — partial/legacy boost
				BudgetID:   "", // deliberately empty — partial/legacy boost
			},
		},
		ConditionLogic: "AND",
		Actions:        actionsJSON,
	}

	// ExecuteBoost must NOT return an error — the condition simply doesn't match.
	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	assert.NoError(t, err, "ExecuteBoost must not brick when CategoryID is empty; boost should evaluate to no-match")
}

// TestEvaluateConditionWithValue_BetweenIsNonEvaluable verifies that a condition with the
// "between" operator evaluates to false at execution time (default-false path in the switch).
// Validation rejects "between" before rules reach execution, but this pins the evaluator
// so that any future inadvertent "between" condition can't accidentally return true.
// Issue #1270 (Codex LOW finding — evaluator default-false path untested).
func TestEvaluateConditionWithValue_BetweenIsNonEvaluable(t *testing.T) {
	condition := domain.BoostCondition{
		Field:    "budget.percent_used",
		Operator: domain.ConditionOperatorBetween,
		Value:    "50,80",
	}

	// fieldValue clearly within the "50 to 80" range — if between were evaluable it
	// would return true; the non-evaluable default must return false.
	result := evaluateConditionWithValue(condition, 65.0)
	assert.False(t, result,
		"between operator must be non-evaluable at execution time (default-false path)")

	// Also verify with a value outside any plausible range — must still be false.
	result2 := evaluateConditionWithValue(condition, 99.0)
	assert.False(t, result2,
		"between operator must be non-evaluable regardless of field value")
}

// TestAllowedOperatorsAreEvaluable asserts parity between the validation-time
// allowlist (domain.AllowedConditionOperators) and the execution-time evaluator
// (evaluateConditionWithValue). Every operator in the allowlist must return true
// for a value/condition pair that should logically match — ensuring no operator
// can be added to the allowlist without also being handled in the evaluator.
// Issue #1271 (Item 7 — A-CR3 parity test).
func TestAllowedOperatorsAreEvaluable(t *testing.T) {
	type probe struct {
		fieldValue     interface{}
		conditionValue interface{}
	}

	// For each allowed operator, supply a (fieldValue, conditionValue) pair that
	// logically SHOULD match — a false result means the operator falls into the
	// default-false branch and is therefore not evaluable.
	probes := map[domain.ConditionOperator]probe{
		domain.ConditionOperatorEq:       {fieldValue: 50.0, conditionValue: "50"},
		domain.ConditionOperatorNeq:      {fieldValue: 50.0, conditionValue: "99"},
		domain.ConditionOperatorGt:       {fieldValue: 50.0, conditionValue: "40"},
		domain.ConditionOperatorGte:      {fieldValue: 50.0, conditionValue: "50"},
		domain.ConditionOperatorLt:       {fieldValue: 50.0, conditionValue: "60"},
		domain.ConditionOperatorLte:      {fieldValue: 50.0, conditionValue: "50"},
		domain.ConditionOperatorContains: {fieldValue: "hello world", conditionValue: "hello"},
	}

	for op := range domain.AllowedConditionOperators() {
		op := op // capture for parallel sub-tests
		p, ok := probes[op]
		if !ok {
			t.Errorf("operator %q is in AllowedConditionOperators but has no probe in this test — add one", op)
			continue
		}

		t.Run(string(op), func(t *testing.T) {
			condition := domain.BoostCondition{
				Field:    "budget.percent_used",
				Operator: op,
				Value:    p.conditionValue,
			}
			result := evaluateConditionWithValue(condition, p.fieldValue)
			assert.True(t, result,
				"operator %q is in AllowedConditionOperators but evaluateConditionWithValue returned false — it is not evaluable", op)
		})
	}
}

// =============================================================================
// ProcessEventTrigger Budget Scoping Tests (A-CR2)
// =============================================================================

// TestProcessEventTrigger_SkipsRuleBoundToOtherBudget verifies that a rule whose
// budget_category.* condition binds budget-X is NOT executed when the event carries
// budget-Y — the A-CR2 OR-escape fix.
func TestProcessEventTrigger_SkipsRuleBoundToOtherBudget(t *testing.T) {
	ctx := context.Background()
	userID := "user-scope-1"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "account-1",
		ToAccountID:   "account-2",
		AmountType:    "fixed",
		Amount:        100.0,
	})

	boostRepo := &mockBoostsRepository{
		boosts: []*domain.Boost{
			{
				ID:        "rule-budget-scope",
				UserID:    userID,
				Name:      "Category Rule for Budget X",
				Enabled:   true,
				Priority:  1,
				BoostType: domain.BoostTypeTransferSuggestion,
				TriggerConfig: domain.TriggerConfig{
					Type:  domain.TriggerTypeEvent,
					Event: domain.EventBudgetPeriodEnd,
				},
				Conditions: []domain.BoostCondition{
					{Field: "budget_category.percent_used", Operator: domain.ConditionOperatorGte, Value: 80.0, CategoryID: "cat-1", BudgetID: "budget-X"},
				},
				ConditionLogic: "AND",
				Actions:        actionsJSON,
			},
		},
	}

	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	// Event is for budget-Y, not budget-X
	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-Y",
	}

	err := service.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)
	require.NoError(t, err)
	// Rule must be entirely skipped — no execution record, no suggestion
	assert.Empty(t, execRepo.executions, "rule bound to budget-X must be skipped when event is for budget-Y")
	assert.Empty(t, suggestionRepo.suggestions, "no suggestion must be created when budget scope does not match")
}

// TestProcessEventTrigger_FiresRuleBoundToMatchingBudget verifies that a rule whose
// budget_category.* condition binds budget-X IS executed when the event carries budget-X.
func TestProcessEventTrigger_FiresRuleBoundToMatchingBudget(t *testing.T) {
	ctx := context.Background()
	userID := "user-scope-2"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "account-1",
		ToAccountID:   "account-2",
		AmountType:    "fixed",
		Amount:        100.0,
	})

	boostRepo := &mockBoostsRepository{
		boosts: []*domain.Boost{
			{
				ID:        "rule-budget-match",
				UserID:    userID,
				Name:      "Category Rule for Budget X",
				Enabled:   true,
				Priority:  1,
				BoostType: domain.BoostTypeTransferSuggestion,
				TriggerConfig: domain.TriggerConfig{
					Type:  domain.TriggerTypeEvent,
					Event: domain.EventBudgetPeriodEnd,
				},
				Conditions: []domain.BoostCondition{
					{Field: "budget_category.percent_used", Operator: domain.ConditionOperatorGte, Value: 80.0, CategoryID: "cat-1", BudgetID: "budget-X"},
				},
				ConditionLogic: "AND",
				Actions:        actionsJSON,
			},
		},
	}

	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	// Event is for the matching budget-X; populate categories so condition evaluates
	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-X",
		domain.EventDataKeyCategories: map[string]interface{}{
			"cat-1": map[string]interface{}{
				"percent_used": 90.0,
			},
		},
	}

	err := service.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)
	require.NoError(t, err)
	// Rule must have been executed — suggestion repo should have a new entry
	assert.NotEmpty(t, suggestionRepo.suggestions, "rule bound to budget-X must fire when event is also for budget-X")
}

// TestProcessEventTrigger_UnscopedRuleFiresForAnyBudget verifies that a rule with no
// budget_category.* condition (not budget-scoped) fires regardless of the event's budget_id.
func TestProcessEventTrigger_UnscopedRuleFiresForAnyBudget(t *testing.T) {
	ctx := context.Background()
	userID := "user-scope-3"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "account-1",
		ToAccountID:   "account-2",
		AmountType:    "fixed",
		Amount:        100.0,
	})

	boostRepo := &mockBoostsRepository{
		boosts: []*domain.Boost{
			{
				ID:        "rule-unscoped",
				UserID:    userID,
				Name:      "Unscoped Budget Rule",
				Enabled:   true,
				Priority:  1,
				BoostType: domain.BoostTypeTransferSuggestion,
				TriggerConfig: domain.TriggerConfig{
					Type:  domain.TriggerTypeEvent,
					Event: domain.EventBudgetPeriodEnd,
				},
				// Only budget.* condition — no budget_category.* so no budget binding
				Conditions: []domain.BoostCondition{
					{Field: "budget.percent_used", Operator: domain.ConditionOperatorGte, Value: 50.0},
				},
				ConditionLogic: "AND",
				Actions:        actionsJSON,
			},
		},
	}

	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID:    "budget-any",
		domain.EventDataKeyPercentUsed: 75.0,
	}

	err := service.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)
	require.NoError(t, err)
	assert.NotEmpty(t, suggestionRepo.suggestions, "unscoped rule must fire for any budget_id")
}

// TestProcessEventTrigger_ORLogicDoesNotEscapeBudgetScope verifies the A-CR2 fix:
// even with condition_logic=OR, a rule bound to budget-X must be fully skipped when
// the event's budget_id is budget-Y. The OR logic must not bypass the scope guard.
func TestProcessEventTrigger_ORLogicDoesNotEscapeBudgetScope(t *testing.T) {
	ctx := context.Background()
	userID := "user-scope-4"

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "account-1",
		ToAccountID:   "account-2",
		AmountType:    "fixed",
		Amount:        100.0,
	})

	boostRepo := &mockBoostsRepository{
		boosts: []*domain.Boost{
			{
				ID:        "rule-or-escape",
				UserID:    userID,
				Name:      "OR Rule With Budget Scope",
				Enabled:   true,
				Priority:  1,
				BoostType: domain.BoostTypeTransferSuggestion,
				TriggerConfig: domain.TriggerConfig{
					Type:  domain.TriggerTypeEvent,
					Event: domain.EventBudgetPeriodEnd,
				},
				// OR logic: second condition (budget.percent_used) would be "true-ish" for any
				// event carrying percent_used ≥ 0, but the rule is still budget-X-scoped via the
				// first condition; it must be skipped entirely before any OR evaluation.
				Conditions: []domain.BoostCondition{
					{Field: "budget_category.percent_used", Operator: domain.ConditionOperatorGte, Value: 80.0, CategoryID: "cat-1", BudgetID: "budget-X"},
					{Field: "budget.percent_used", Operator: domain.ConditionOperatorGte, Value: 0.0},
				},
				ConditionLogic: "OR",
				Actions:        actionsJSON,
			},
		},
	}

	service := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	// Event is for budget-Y; percent_used satisfies the second condition but budget scope blocks
	eventData := map[string]interface{}{
		domain.EventDataKeyBudgetID:    "budget-Y",
		domain.EventDataKeyPercentUsed: 90.0,
	}

	err := service.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)
	require.NoError(t, err)
	assert.Empty(t, execRepo.executions, "OR-logic must not bypass budget scope guard (A-CR2)")
	assert.Empty(t, suggestionRepo.suggestions, "no suggestion must be created when budget scope blocks the rule")
}

// =============================================================================
// Issue #1271 Items 5+6: dual-shape reader, json.Number, budget-scope, key contract
// =============================================================================

// TestLookupFieldValue_TypedFloatShape verifies that lookupFieldValue resolves a
// budget_category.spent_amount condition when the categories map uses the strongly-typed
// map[string]map[string]float64 shape that BuildCategoryEventData now emits.
func TestLookupFieldValue_TypedFloatShape(t *testing.T) {
	condition := domain.BoostCondition{
		Field:      "budget_category.spent_amount",
		CategoryID: "cat-1",
		BudgetID:   "budget-X",
		Operator:   domain.ConditionOperatorGt,
		Value:      "100",
	}
	data := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-X",
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"cat-1": {
				"spent":        200.0,
				"remaining":    300.0,
				"percent_used": 40.0,
				"allocated":    500.0,
			},
		},
	}
	got := evaluateCondition(condition, data)
	assert.True(t, got, "typed float64 shape must resolve spent_amount → spent key")
}

// TestLookupFieldValue_BudgetScopeMismatchReturnsFalse verifies that when a condition's
// BudgetID does not match the event's budget_id, lookupFieldValue returns false.
func TestLookupFieldValue_BudgetScopeMismatchReturnsFalse(t *testing.T) {
	condition := domain.BoostCondition{
		Field:      "budget_category.spent_amount",
		CategoryID: "cat-1",
		BudgetID:   "budget-Y", // mismatch: event carries budget-X
		Operator:   domain.ConditionOperatorGt,
		Value:      "100",
	}
	data := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-X",
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"cat-1": {"spent": 200.0, "remaining": 300.0, "percent_used": 40.0, "allocated": 500.0},
		},
	}
	got := evaluateCondition(condition, data)
	assert.False(t, got, "budget scope mismatch must make condition return false")
}

// TestCompareNumeric_JSONNumber verifies that compareNumeric handles json.Number values
// that arrive after a River JSON round-trip (JSON unmarshal into map[string]interface{}).
func TestCompareNumeric_JSONNumber(t *testing.T) {
	condition := domain.BoostCondition{
		Field:      "budget_category.percent_used",
		CategoryID: "cat-1",
		BudgetID:   "budget-X",
		Operator:   domain.ConditionOperatorGt,
		Value:      "40",
	}
	data := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-X",
		domain.EventDataKeyCategories: map[string]interface{}{
			"cat-1": map[string]interface{}{
				"percent_used": json.Number("42.5"),
			},
		},
	}
	got := evaluateCondition(condition, data)
	assert.True(t, got, "json.Number must be handled by compareNumeric after River JSON round-trip")
}

// TestCategoryConditionFires_AfterRiverJSONRoundTrip simulates the full River
// JSON round-trip: typed map → marshal → unmarshal into interface{} → evaluate.
// This catches the json.Number issue that arises when River re-reads a job's args.
func TestCategoryConditionFires_AfterRiverJSONRoundTrip(t *testing.T) {
	// Build EventData with the strongly-typed categories map
	original := map[string]interface{}{
		domain.EventDataKeyBudgetID: "budget-X",
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"cat-1": {
				"percent_used": 35.0,
				"remaining":    650.0,
				"spent":        350.0,
				"allocated":    1000.0,
			},
		},
	}

	// Marshal then unmarshal (River JSON round-trip)
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	var roundTripped map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &roundTripped))

	condition := domain.BoostCondition{
		Field:      "budget_category.percent_used",
		CategoryID: "cat-1",
		BudgetID:   "budget-X",
		Operator:   domain.ConditionOperatorGt,
		Value:      "30",
	}
	got := evaluateCondition(condition, roundTripped)
	assert.True(t, got, "category condition must evaluate correctly after River JSON round-trip")
}

// TestBudgetTotalSpentStillResolves is a regression guard that verifies the budget-level
// total_spent field (emitted as domain.EventDataKeyTotalSpent) still resolves correctly.
// This is separate from the category-level "spent" key rename.
func TestBudgetTotalSpentStillResolves(t *testing.T) {
	condition := domain.BoostCondition{
		Field:    "budget.total_spent",
		Operator: domain.ConditionOperatorGt,
		Value:    "400",
	}
	data := map[string]interface{}{
		domain.EventDataKeyTotalSpent: 500.0,
		domain.EventDataKeyBudgetID:   "budget-X",
	}
	got := evaluateCondition(condition, data)
	assert.True(t, got, "budget.total_spent must still resolve via EventDataKeyTotalSpent")
}

// TestCategoryEventDataKeyContract verifies that BuildCategoryEventData emits exactly
// the keys {"percent_used", "remaining", "spent", "allocated", "is_sub_allocation"} — no
// more, no less. This is a parity/lock test: if the emitter shape changes, this test
// fails immediately.
// Issue #1271 Item 5: key contract enforcement.
// Issue #1630 Task 3.6: "is_sub_allocation" added deliberately — it marks whether a row's
// dollars are already reflected in a budgeted parent, so a rollover boost consuming this
// data can tell a sub-allocation apart from a top-level row and avoid rolling the same
// dollars twice. Updating this lock test's expected key set is the intended contract
// change, not drift.
func TestCategoryEventDataKeyContract(t *testing.T) {
	cat := domain.BudgetCategory{
		ID:              "cat-contract",
		BudgetID:        "budget-contract",
		AllocatedAmount: 200.0,
		SpentAmount:     80.0,
	}
	result := domain.BuildCategoryEventData([]domain.BudgetCategory{cat})

	catMap, ok := result["cat-contract"]
	require.True(t, ok, "expected category key in result")

	expectedKeys := map[string]bool{
		"percent_used":      true,
		"remaining":         true,
		"spent":             true,
		"allocated":         true,
		"is_sub_allocation": true,
	}
	// Every expected key must be present
	for k := range expectedKeys {
		assert.Contains(t, catMap, k, "expected key %q in category event data", k)
	}
	// No extra keys
	assert.Len(t, catMap, len(expectedKeys), "category event data must have exactly %d keys", len(expectedKeys))
}

// ===========================================================================
// Priority 1: SetAnalytics, SetNotificationRepo — zero coverage
// ===========================================================================

// mockAnalyticsForBoostExec is a minimal phclient.Analytics mock for setter tests.
type mockAnalyticsForBoostExec struct{}

func (m *mockAnalyticsForBoostExec) Capture(distinctID, event string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForBoostExec) Identify(distinctID string, properties map[string]interface{}) {}
func (m *mockAnalyticsForBoostExec) Close()                                                        {}

var _ phclient.Analytics = (*mockAnalyticsForBoostExec)(nil)

// mockNotificationRepoForExec is a minimal repository.NotificationRepository mock for setter tests.
type mockNotificationRepoForExec struct{}

func (m *mockNotificationRepoForExec) Create(ctx context.Context, notification *domain.Notification) error {
	return nil
}
func (m *mockNotificationRepoForExec) Update(ctx context.Context, notification *domain.Notification) error {
	return nil
}
func (m *mockNotificationRepoForExec) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	return nil, nil
}
func (m *mockNotificationRepoForExec) MarkAsRead(ctx context.Context, id, userID string) error {
	return nil
}
func (m *mockNotificationRepoForExec) MarkAllAsRead(ctx context.Context, userID string) (int64, error) {
	return 0, nil
}
func (m *mockNotificationRepoForExec) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	return 0, nil
}
func (m *mockNotificationRepoForExec) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	return nil, nil
}
func (m *mockNotificationRepoForExec) GetOpenByTransferSuggestionID(ctx context.Context, transferSuggestionID string, notificationType string) (*domain.Notification, error) {
	return nil, nil
}

var _ repository.NotificationRepository = (*mockNotificationRepoForExec)(nil)

// TestBoostExecutionService_SetAnalytics verifies SetAnalytics assigns the analytics client.
func TestBoostExecutionService_SetAnalytics(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	analytics := &mockAnalyticsForBoostExec{}
	require.NotPanics(t, func() {
		svc.SetAnalytics(analytics)
	}, "SetAnalytics must not panic")
}

// TestBoostExecutionService_SetNotificationRepo verifies SetNotificationRepo assigns the repo.
func TestBoostExecutionService_SetNotificationRepo(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	notifRepo := &mockNotificationRepoForExec{}
	require.NotPanics(t, func() {
		svc.SetNotificationRepo(notifRepo)
	}, "SetNotificationRepo must not panic")
}

// ===========================================================================
// Priority 1: containsValue and ptrTime pure utility functions
// ===========================================================================

// TestContainsValue verifies containsValue parses comma-separated lists correctly.
func TestContainsValue(t *testing.T) {
	tests := []struct {
		list     string
		value    string
		expected bool
	}{
		{"a,b,c", "b", true},
		{"a,b,c", "d", false},
		{"", "a", false},
		{" a , b , c ", "a", true},
		{" a , b , c ", "c", true},
		{" a , b , c ", "d", false},
		{"single", "single", true},
		{"single", "other", false},
	}

	for _, tc := range tests {
		result := containsValue(tc.list, tc.value)
		assert.Equal(t, tc.expected, result,
			"containsValue(%q, %q) should be %v", tc.list, tc.value, tc.expected)
	}
}

// TestPtrTime verifies ptrTime returns a non-nil pointer equal to the input.
func TestPtrTime(t *testing.T) {
	now := time.Now()
	result := ptrTime(now)

	require.NotNil(t, result, "ptrTime must return non-nil pointer")
	assert.Equal(t, now, *result, "ptrTime result must equal input time")
}

// ===========================================================================
// Priority 2: GetExecutionHistoryWithFilter — zero coverage
// ===========================================================================

// TestBoostExecutionService_GetExecutionHistoryWithFilter_NoFilter verifies unfiltered results.
func TestBoostExecutionService_GetExecutionHistoryWithFilter_NoFilter(t *testing.T) {
	ctx := context.Background()
	boostID := "rule-1"

	execRepo := &mockBoostExecutionRepository{
		executions: []*domain.BoostExecution{
			{ID: "e1", BoostID: &boostID, Status: domain.ExecutionStatusCompleted},
			{ID: "e2", BoostID: &boostID, Status: domain.ExecutionStatusFailed},
		},
	}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	results, total, err := svc.GetExecutionHistoryWithFilter(ctx, boostID, nil, 10, 0)

	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, results, 2)
}

// TestBoostExecutionService_GetExecutionHistoryWithFilter_WithStatusFilter verifies status filtering.
func TestBoostExecutionService_GetExecutionHistoryWithFilter_WithStatusFilter(t *testing.T) {
	ctx := context.Background()
	boostID := "rule-1"
	status := domain.ExecutionStatusCompleted

	execRepo := &mockBoostExecutionRepository{
		executions: []*domain.BoostExecution{
			{ID: "e1", BoostID: &boostID, Status: domain.ExecutionStatusCompleted},
			{ID: "e2", BoostID: &boostID, Status: domain.ExecutionStatusFailed},
			{ID: "e3", BoostID: &boostID, Status: domain.ExecutionStatusCompleted},
		},
	}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	results, total, err := svc.GetExecutionHistoryWithFilter(ctx, boostID, &status, 10, 0)

	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, results, 2)
	for _, r := range results {
		assert.Equal(t, domain.ExecutionStatusCompleted, r.Status)
	}
}

// ---------------------------------------------------------------------------------------------
// Issue #1569 Task 14 — the definition snapshot is captured at execution time and frozen there.
// ---------------------------------------------------------------------------------------------

// newSnapshotTestBoost builds a fully-populated budget-alert boost for the snapshot tests.
func newSnapshotTestBoost() *domain.Boost {
	actionsJSON, _ := json.Marshal(map[string]interface{}{
		"notification_channels": []string{"email"},
		"threshold_type":        "percentage",
		"threshold_value":       80,
	})
	desc := "explains itself to the user, not to the audit trail"
	return &domain.Boost{
		ID:          "boost-snapshot-1569",
		UserID:      "user-snapshot-1569",
		Name:        "Warn me at 80%",
		Description: &desc,
		Enabled:     true,
		Priority:    7,
		BoostType:   domain.BoostTypeBudgetAlert,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []domain.BoostCondition{{
			Field:    "budget_category.percent_used",
			Operator: domain.ConditionOperatorGte,
			Value:    "80",
		}},
		ConditionLogic: "AND",
		Actions:        actionsJSON,
	}
}

// TestExecuteBoost_WritesDefinitionSnapshotAtExecutionTime is the "what did this automation do, and
// why" half. fk_boost_executions_boost_id is ON DELETE SET NULL, so the execution row has to carry
// its own explanation; a row that only says `status=completed` explains nothing once the boost that
// produced it is gone.
func TestExecuteBoost_WritesDefinitionSnapshotAtExecutionTime(t *testing.T) {
	ctx := context.Background()
	execRepo := &mockBoostExecutionRepository{}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	boost := newSnapshotTestBoost()
	execution, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, execution)

	assert.Equal(t, boost.UserID, execution.UserID,
		"user_id is the execution row's own ON DELETE CASCADE path to users once boost_id can be NULL")
	require.NotNil(t, execution.BoostID)
	assert.Equal(t, boost.ID, *execution.BoostID)
	require.NotNil(t, execution.BoostName)
	assert.Equal(t, "Warn me at 80%", *execution.BoostName)

	require.NotEmpty(t, execution.DefinitionSnapshot, "an orphan with no snapshot explains nothing")
	var snap map[string]interface{}
	require.NoError(t, json.Unmarshal(execution.DefinitionSnapshot, &snap))

	assert.Equal(t, boost.ID, snap["boost_id"], "which automation this was, after boost_id is NULLed")
	assert.Equal(t, "Warn me at 80%", snap["name"])
	assert.Equal(t, string(domain.BoostTypeBudgetAlert), snap["boost_type"], "WHAT it did")
	assert.NotNil(t, snap["actions"], "WHAT it did, in detail")
	assert.NotNil(t, snap["trigger_config"], "WHY it ran at all")
	assert.NotEmpty(t, snap["conditions"], "WHY it fired")
	assert.Equal(t, "AND", snap["condition_logic"])
	assert.Equal(t, true, snap["enabled"], "why a skip was recorded, when one is")
	assert.Equal(t, float64(7), snap["priority"])
	assert.Equal(t, "boost_execution_service", snap["captured_by"],
		"provenance distinguishes a live capture from migration 000164's backfill")
	assert.NotEmpty(t, snap["captured_at"])

	// Deliberate exclusions, asserted so a later "just add everything" does not slip in.
	assert.NotContains(t, snap, "description",
		"free text with no bearing on behaviour and a PII surface with no reader")
	assert.NotContains(t, snap, "execution_count",
		"a lifetime counter, not a fact about this execution")
	assert.NotContains(t, snap, "last_executed_at")
}

// TestExecuteBoost_SnapshotDoesNotFollowLaterDefinitionEdits is the "as it was THEN" half, and the
// only thing that makes the snapshot worth having: if editing the boost rewrote its history, the
// snapshot would be a slower way of reading the parent row.
func TestExecuteBoost_SnapshotDoesNotFollowLaterDefinitionEdits(t *testing.T) {
	ctx := context.Background()
	execRepo := &mockBoostExecutionRepository{}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	boost := newSnapshotTestBoost()
	execution, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)

	// The user renames the boost, disables it, raises the threshold and drops the condition.
	boost.Name = "Warn me at 95%"
	boost.Enabled = false
	boost.Priority = 99
	boost.Conditions = nil
	boost.Actions, _ = json.Marshal(map[string]interface{}{
		"notification_channels": []string{"email"},
		"threshold_type":        "percentage",
		"threshold_value":       95,
	})

	// A second execution must see the NEW definition...
	second, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, second.BoostName)
	assert.Equal(t, "Warn me at 95%", *second.BoostName)

	// ...while the first execution's record of what happened is untouched.
	require.Len(t, execRepo.executions, 2)
	stored := execRepo.executions[0]
	require.Equal(t, execution.ID, stored.ID)
	require.NotNil(t, stored.BoostName)
	assert.Equal(t, "Warn me at 80%", *stored.BoostName,
		"renaming a boost must not rewrite what the audit trail says it was called when it ran")

	var snap map[string]interface{}
	require.NoError(t, json.Unmarshal(stored.DefinitionSnapshot, &snap))
	assert.Equal(t, "Warn me at 80%", snap["name"])
	assert.Equal(t, true, snap["enabled"], "the boost was enabled WHEN IT RAN; it is disabled now")
	assert.Equal(t, float64(7), snap["priority"])
	assert.NotEmpty(t, snap["conditions"], "the conditions it actually evaluated, not today's empty set")
	actions, ok := snap["actions"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(80), actions["threshold_value"],
		"the threshold it actually applied, not the one the user set afterwards")
}

// ---------------------------------------------------------------------------------------------
// Issue #1569 Task 14 (review round 2) — the per-user automation history reader.
// ---------------------------------------------------------------------------------------------

// TestGetUserExecutionHistory_ReturnsOrphansAndScopesToTheUser is the service half of the only read
// path that can surface an execution whose boost has been purged. Every other read is keyed on
// boost_id, so without this a retained orphan is invisible to every client.
func TestGetUserExecutionHistory_ReturnsOrphansAndScopesToTheUser(t *testing.T) {
	ctx := context.Background()

	orphanName := "Purged Boost"
	liveBoostID := "boost-live"
	execRepo := &mockBoostExecutionRepository{
		executions: []*domain.BoostExecution{
			// The orphan: its boost is gone, so boost_id is NULL.
			{ID: "e-orphan", BoostID: nil, UserID: "user-1", BoostName: &orphanName,
				Status: domain.ExecutionStatusCompleted},
			{ID: "e-live", BoostID: &liveBoostID, UserID: "user-1",
				Status: domain.ExecutionStatusCompleted},
			// Another tenant's row. Must never appear.
			{ID: "e-other", BoostID: &liveBoostID, UserID: "user-2",
				Status: domain.ExecutionStatusCompleted},
		},
	}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	executions, total, err := svc.GetUserExecutionHistory(ctx, "user-1", nil, 20, 0)
	require.NoError(t, err)

	assert.Equal(t, 2, total)
	require.Len(t, executions, 2)

	ids := make([]string, 0, len(executions))
	for _, e := range executions {
		ids = append(ids, e.ID)
	}
	assert.Contains(t, ids, "e-orphan",
		"the orphan is the whole reason this reader exists; filtering it out is the bug")
	assert.NotContains(t, ids, "e-other", "another tenant's automation history must never be returned")

	var orphan *domain.BoostExecution
	for _, e := range executions {
		if e.ID == "e-orphan" {
			orphan = e
		}
	}
	require.NotNil(t, orphan)
	assert.Nil(t, orphan.BoostID, "the parent link is gone")
	require.NotNil(t, orphan.BoostName)
	assert.Equal(t, "Purged Boost", *orphan.BoostName,
		"and the caller can still say which automation this was")
}

func TestGetUserExecutionHistory_PassesTheStatusFilterThrough(t *testing.T) {
	ctx := context.Background()
	liveBoostID := "boost-live"
	execRepo := &mockBoostExecutionRepository{
		executions: []*domain.BoostExecution{
			{ID: "e-done", BoostID: &liveBoostID, UserID: "user-1", Status: domain.ExecutionStatusCompleted},
			{ID: "e-failed", BoostID: nil, UserID: "user-1", Status: domain.ExecutionStatusFailed},
		},
	}
	svc := NewBoostExecutionService(execRepo, &mockTransferSuggestionRepository{}, &mockBoostsRepository{}, nil)

	failed := domain.ExecutionStatusFailed
	executions, total, err := svc.GetUserExecutionHistory(ctx, "user-1", &failed, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, executions, 1)
	assert.Equal(t, "e-failed", executions[0].ID)
}

// ===========================================================================
// Issue #1630 Task 3.6: rollover boosts must not roll a sub-allocation's dollars
// ===========================================================================

// TestExecuteCategoryRollover_SkipsSubAllocationCategory proves that a category_rollover
// boost whose SourceBudgetCategoryID names a category marked "is_sub_allocation": 1.0 in
// the per-category event data creates NO suggestion. The child's dollars are already
// reflected in its budgeted parent's own remaining/spent (Issue #1630); rolling the
// child independently would roll those same dollars a second time.
//
// RED (pre-fix): executeCategoryRollover reads only the flat top-level "remaining" key
// and never consults the per-category "is_sub_allocation" marker, so it creates a
// suggestion regardless of hierarchy.
func TestExecuteCategoryRollover_SkipsSubAllocationCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover-suballoc"

	actionsJSON, err := json.Marshal(domain.RolloverAction{
		SourceBudgetCategoryID: "child-cat",
		RolloverType:           "full",
	})
	require.NoError(t, err)

	boost := &domain.Boost{
		ID:             "rule-rollover-suballoc",
		UserID:         userID,
		Name:           "Rollover child category",
		Enabled:        true,
		BoostType:      domain.BoostTypeCategoryRollover,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
	}

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	triggerData := map[string]interface{}{
		"remaining": 300.0, // whole-budget remaining; nonzero so a naive read would roll it
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"child-cat": {
				"percent_used": 50.0,
				"remaining":    30.0,
				"spent":        30.0,
				"allocated":    60.0,
				domain.EventDataCategoryKeyIsSubAllocation: 1.0,
			},
		},
	}

	execution, err := svc.ExecuteBoost(ctx, boost, triggerData)
	require.NoError(t, err, "ExecuteBoost must not error for a skipped sub-allocation rollover")
	assert.Empty(t, suggestionRepo.suggestions,
		"a rollover boost on a sub-allocation category must not create a suggestion")

	// Issue #1630 B6: reporting success (ExecutionStatusCompleted, execution_count incremented,
	// last_successful touched) while doing nothing is the bug — this must be recorded as a
	// SKIP, exactly like "boost disabled" / "conditions not met".
	require.NotNil(t, execution)
	assert.Equal(t, ExecutionStatusSkipped, execution.Status,
		"a rollover on a sub-allocation source must be recorded as SKIPPED, not completed")
	assert.False(t, boostRepo.incrementExecCalled,
		"a skipped rollover must not increment execution_count")
	assert.False(t, boostRepo.updateSuccessCalled,
		"a skipped rollover must not update last_successful")
}

// TestExecuteCategoryRollover_SkipsSubAllocationCategory_AfterRiverJSONRoundTrip proves the
// same skip behaviour holds when the categories map has been through a River JSON
// round-trip (map[string]interface{} shape, numeric values as json.Number/float64).
func TestExecuteCategoryRollover_SkipsSubAllocationCategory_AfterRiverJSONRoundTrip(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover-suballoc-rt"

	actionsJSON, err := json.Marshal(domain.RolloverAction{
		SourceBudgetCategoryID: "child-cat",
		RolloverType:           "full",
	})
	require.NoError(t, err)

	boost := &domain.Boost{
		ID:             "rule-rollover-suballoc-rt",
		UserID:         userID,
		Name:           "Rollover child category (round-tripped)",
		Enabled:        true,
		BoostType:      domain.BoostTypeCategoryRollover,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
	}

	original := map[string]interface{}{
		"remaining": 300.0,
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"child-cat": {
				"percent_used": 50.0,
				"remaining":    30.0,
				"spent":        30.0,
				"allocated":    60.0,
				domain.EventDataCategoryKeyIsSubAllocation: 1.0,
			},
		},
	}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	var roundTripped map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &roundTripped))

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	execution, err := svc.ExecuteBoost(ctx, boost, roundTripped)
	require.NoError(t, err, "ExecuteBoost must not error for a skipped sub-allocation rollover")
	assert.Empty(t, suggestionRepo.suggestions,
		"a rollover boost on a sub-allocation category must not create a suggestion, even after a River JSON round-trip")

	require.NotNil(t, execution)
	assert.Equal(t, ExecutionStatusSkipped, execution.Status)
	assert.False(t, boostRepo.incrementExecCalled)
	assert.False(t, boostRepo.updateSuccessCalled)
}

// TestExecuteCategoryRollover_StillRollsTopLevelCategory is a regression guard: a
// category_rollover boost on a TOP-LEVEL category (is_sub_allocation: 0.0, or the key
// absent entirely for a legacy/flat trigger payload) must still create a suggestion.
// The fix must not turn every rollover boost into a no-op.
func TestExecuteCategoryRollover_StillRollsTopLevelCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover-toplevel"

	actionsJSON, err := json.Marshal(domain.RolloverAction{
		SourceBudgetCategoryID: "parent-cat",
		RolloverType:           "full",
	})
	require.NoError(t, err)

	boost := &domain.Boost{
		ID:             "rule-rollover-toplevel",
		UserID:         userID,
		Name:           "Rollover parent category",
		Enabled:        true,
		BoostType:      domain.BoostTypeCategoryRollover,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
	}

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	triggerData := map[string]interface{}{
		"remaining": 300.0,
		domain.EventDataKeyCategories: map[string]map[string]float64{
			"parent-cat": {
				"percent_used": 60.0,
				"remaining":    200.0,
				"spent":        300.0,
				"allocated":    500.0,
				domain.EventDataCategoryKeyIsSubAllocation: 0.0,
			},
		},
	}

	_, err = svc.ExecuteBoost(ctx, boost, triggerData)
	require.NoError(t, err)
	assert.Len(t, suggestionRepo.suggestions, 1,
		"a rollover boost on a top-level (non-sub-allocation) category must still create a suggestion")
}

// TestExecuteCategoryRollover_NoCategoriesMapStillRolls is a backward-compatibility
// regression guard: a legacy/flat trigger payload with no "categories" key at all
// (e.g. a hand-built test payload, or an emitter that predates Issue #1270) must still
// let a rollover boost fire using the flat "remaining" key, unaffected by the new
// sub-allocation check.
func TestExecuteCategoryRollover_NoCategoriesMapStillRolls(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover-flat"

	actionsJSON, err := json.Marshal(domain.RolloverAction{
		SourceBudgetCategoryID: "cat-1",
		RolloverType:           "full",
	})
	require.NoError(t, err)

	boost := &domain.Boost{
		ID:             "rule-rollover-flat",
		UserID:         userID,
		Name:           "Rollover flat payload",
		Enabled:        true,
		BoostType:      domain.BoostTypeCategoryRollover,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "budget_period_end"},
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
	}

	execRepo := &mockBoostExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}
	svc := NewBoostExecutionService(execRepo, suggestionRepo, boostRepo, nil)

	_, err = svc.ExecuteBoost(ctx, boost, map[string]interface{}{"remaining": 200.0})
	require.NoError(t, err)
	assert.Len(t, suggestionRepo.suggestions, 1,
		"a rollover boost must still fire against a flat payload with no categories map")
}
