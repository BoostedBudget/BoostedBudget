// Package services provides business logic for rule execution.
// Issue #35: Rules Engine Core - condition evaluation and action execution.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// RuleActions is a wrapper for parsing action configurations from JSONB.
type RuleActions struct {
	Transfer    *domain.TransferAction    `json:"transfer,omitempty"`
	Alert       *domain.AlertAction       `json:"alert,omitempty"`
	Rollover    *domain.RolloverAction    `json:"rollover,omitempty"`
	SavingsGoal *domain.SavingsGoalAction `json:"savings_goal,omitempty"`
}

// ExecutionStatusSkipped is a synthetic status for skipped executions.
// In the database, this maps to "completed" with output_data containing skip_reason.
// This allows us to track skipped executions separately in the service layer.
const ExecutionStatusSkipped domain.ExecutionStatus = "completed"

// RuleExecutionService defines the interface for rule execution business logic.
type RuleExecutionService interface {
	// ExecuteRule executes a single rule with the given trigger data.
	ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error)

	// ProcessEventTrigger processes an event trigger for all matching rules.
	ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error

	// GetExecutionHistory retrieves execution history for a rule.
	GetExecutionHistory(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error)

	// GetExecutionHistoryWithFilter retrieves execution history with optional status filter and total count.
	// Issue #38: Rules API Endpoints - Execution history with filtering.
	GetExecutionHistoryWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error)

	// TestRule performs a dry-run execution of a rule without persisting any changes.
	// Issue #38: Rules API Endpoints - Test endpoint.
	TestRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleTestResult, error)

}

// ruleExecutionService implements RuleExecutionService.
type ruleExecutionService struct {
	executionRepo       repository.RuleExecutionRepository
	suggestionRepo      repository.TransferSuggestionRepository
	ruleRepo            repository.AutomationRulesRepository
	savingsGoalRepo     repository.SavingsGoalRepository
	notificationService NotificationService
	notificationRepo    repository.NotificationRepository
	analytics           phclient.Analytics
	chainResolver       AccountChainResolver // Issue #1109: resolves active account IDs across re-links
}

// NotificationService defines the interface for sending notifications.
type NotificationService interface {
	SendBudgetAlert(ctx context.Context, userID string, alert *domain.AlertAction, data map[string]interface{}) error
}

// NewRuleExecutionService creates a new RuleExecutionService instance.
func NewRuleExecutionService(
	executionRepo repository.RuleExecutionRepository,
	suggestionRepo repository.TransferSuggestionRepository,
	ruleRepo repository.AutomationRulesRepository,
	notificationService NotificationService,
) *ruleExecutionService {
	return &ruleExecutionService{
		executionRepo:       executionRepo,
		suggestionRepo:      suggestionRepo,
		ruleRepo:            ruleRepo,
		notificationService: notificationService,
	}
}

// SetAnalytics sets the analytics client for event capture.
func (s *ruleExecutionService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// SetSavingsGoalRepo sets the savings goal repository for savings goal executor.
func (s *ruleExecutionService) SetSavingsGoalRepo(repo repository.SavingsGoalRepository) {
	s.savingsGoalRepo = repo
}

// SetNotificationRepo sets the notification repository for creating transfer suggestion notifications.
func (s *ruleExecutionService) SetNotificationRepo(repo repository.NotificationRepository) {
	s.notificationRepo = repo
}

// SetChainResolver injects the chain resolver to map user_account_id → active account_id.
// Issue #1109: Used by executeTransferSuggestion and executeSavingsGoal.
func (s *ruleExecutionService) SetChainResolver(resolver AccountChainResolver) {
	s.chainResolver = resolver
}

// ExecuteRule executes a single rule with the given trigger data.
func (s *ruleExecutionService) ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	startTime := time.Now()

	// Create execution record
	triggerContextJSON, _ := json.Marshal(triggerData)
	execution := &domain.RuleExecution{
		ID:             uuid.New().String(),
		RuleID:         rule.ID,
		Status:         domain.ExecutionStatusRunning,
		StartedAt:      startTime,
		TriggerContext: triggerContextJSON,
	}

	// Save initial execution record
	if err := s.executionRepo.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution record: %w", err)
	}

	// Check if rule is enabled
	if !rule.Enabled {
		execution.Status = ExecutionStatusSkipped
		completedAt := time.Now()
		execution.CompletedAt = &completedAt
		durationMs := int(time.Since(startTime).Milliseconds())
		execution.ExecutionDurationMs = &durationMs
		skipReason := "rule disabled"
		execution.OutputData, _ = json.Marshal(map[string]string{"skip_reason": skipReason})
		_ = s.executionRepo.Update(ctx, execution)
		return execution, nil
	}

	// Evaluate conditions
	if !evaluateConditions(rule.Conditions, rule.ConditionLogic, triggerData) {
		execution.Status = ExecutionStatusSkipped
		completedAt := time.Now()
		execution.CompletedAt = &completedAt
		durationMs := int(time.Since(startTime).Milliseconds())
		execution.ExecutionDurationMs = &durationMs
		skipReason := "conditions not met"
		execution.OutputData, _ = json.Marshal(map[string]string{"skip_reason": skipReason})
		_ = s.executionRepo.Update(ctx, execution)
		return execution, nil
	}

	// Execute action based on rule type
	var actionErr error
	switch rule.RuleType {
	case domain.RuleTypeTransferSuggestion:
		actionErr = s.executeTransferSuggestion(ctx, rule, execution.ID, triggerData)
	case domain.RuleTypeBudgetAlert:
		actionErr = s.executeBudgetAlert(ctx, rule, triggerData)
	case domain.RuleTypeCategoryRollover:
		actionErr = s.executeCategoryRollover(ctx, rule, execution.ID, triggerData)
	case domain.RuleTypeSavingsGoal:
		actionErr = s.executeSavingsGoal(ctx, rule, execution.ID, triggerData)
	default:
		actionErr = fmt.Errorf("unknown rule type: %s", rule.RuleType)
	}

	// Update execution record
	completedAt := time.Now()
	execution.CompletedAt = &completedAt
	durationMs := int(time.Since(startTime).Milliseconds())
	execution.ExecutionDurationMs = &durationMs

	if actionErr != nil {
		execution.Status = domain.ExecutionStatusFailed
		errMsg := actionErr.Error()
		execution.ErrorMessage = &errMsg
		errCode := "ACTION_ERROR"
		execution.ErrorCode = &errCode
		_ = s.executionRepo.Update(ctx, execution)
		return execution, actionErr
	}

	execution.Status = domain.ExecutionStatusCompleted
	_ = s.executionRepo.Update(ctx, execution)

	// Update rule execution stats
	_ = s.ruleRepo.IncrementExecutionCount(ctx, rule.ID)
	_ = s.ruleRepo.UpdateLastSuccessful(ctx, rule.ID)

	if s.analytics != nil {
		s.analytics.Capture(rule.UserID, phclient.EventRuleTriggered, map[string]interface{}{
			"rule_id":   rule.ID,
			"rule_type": string(rule.RuleType),
		})
	}

	return execution, nil
}

// ProcessEventTrigger processes an event trigger for all matching rules.
// Simplified: No tier restrictions on event-based triggers.
func (s *ruleExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	// Get matching rules for this event
	rules, err := s.ruleRepo.GetRulesByEventTrigger(ctx, userID, eventType)
	if err != nil {
		return err
	}

	// Execute each matching rule
	for _, rule := range rules {
		_, execErr := s.ExecuteRule(ctx, rule, eventData)
		if execErr != nil {
			// Log error but continue with other rules
			// In production, this would be logged to a proper logging system
			continue
		}
	}

	return nil
}

// GetExecutionHistory retrieves execution history for a rule.
func (s *ruleExecutionService) GetExecutionHistory(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error) {
	return s.executionRepo.GetByRuleID(ctx, ruleID, limit, offset)
}

// GetExecutionHistoryWithFilter retrieves execution history with optional status filter and total count.
// Issue #38: Rules API Endpoints - Execution history with filtering.
func (s *ruleExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error) {
	return s.executionRepo.GetByRuleIDWithFilter(ctx, ruleID, status, limit, offset)
}

// TestRule performs a dry-run execution of a rule without persisting any changes.
// Issue #38: Rules API Endpoints - Test endpoint.
// This method:
// - Evaluates each condition and returns individual results
// - Simulates what action would occur (without persisting)
// - Works on disabled rules for preview purposes
// - Does NOT count toward daily execution limits
// - Does NOT persist execution records
// - Does NOT create transfer suggestions or send notifications
func (s *ruleExecutionService) TestRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleTestResult, error) {
	result := &domain.RuleTestResult{
		RuleID:          rule.ID,
		RuleName:        rule.Name,
		RuleType:        rule.RuleType,
		TriggerDataUsed: triggerData,
	}

	// Evaluate each condition individually
	conditionResults := make([]domain.ConditionEvalResult, len(rule.Conditions))
	for i, condition := range rule.Conditions {
		actual, exists := triggerData[condition.Field]
		met := evaluateCondition(condition, triggerData)

		conditionResults[i] = domain.ConditionEvalResult{
			Field:    condition.Field,
			Operator: string(condition.Operator),
			Value:    condition.Value,
			Actual:   actual,
			Met:      met,
		}

		if !exists {
			conditionResults[i].Actual = nil
		}
	}
	result.ConditionResults = conditionResults

	// Determine overall condition result based on logic
	result.ConditionsMet = evaluateConditions(rule.Conditions, rule.ConditionLogic, triggerData)

	// If conditions are met, simulate the action without persisting
	if result.ConditionsMet {
		simulatedAction, err := s.simulateAction(rule, triggerData)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.SimulatedAction = simulatedAction
		}
	}

	result.PopulatePFCDisplay()
	return result, nil
}

// simulateAction returns a description of what action would occur without actually executing it.
func (s *ruleExecutionService) simulateAction(rule *domain.AutomationRule, triggerData map[string]interface{}) (map[string]interface{}, error) {
	var actions RuleActions
	if err := json.Unmarshal(rule.Actions, &actions); err != nil {
		return nil, fmt.Errorf("failed to parse actions: %w", err)
	}

	simulatedAction := make(map[string]interface{})

	switch rule.RuleType {
	case domain.RuleTypeTransferSuggestion:
		if actions.Transfer == nil {
			return nil, fmt.Errorf("transfer action not defined")
		}
		amount := s.calculateTransferAmount(actions.Transfer, triggerData)
		simulatedAction["action_type"] = "transfer_suggestion"
		simulatedAction["from_account_id"] = actions.Transfer.FromAccountID
		simulatedAction["to_account_id"] = actions.Transfer.ToAccountID
		simulatedAction["amount"] = amount
		simulatedAction["amount_type"] = actions.Transfer.AmountType
		// Issue #1109: Expose stable user_account_id for transparency if available
		if actions.Transfer.FromUserAccountID != "" {
			simulatedAction["from_user_account_id"] = actions.Transfer.FromUserAccountID
		}

	case domain.RuleTypeBudgetAlert:
		if actions.Alert == nil {
			return nil, fmt.Errorf("alert action not defined")
		}
		simulatedAction["action_type"] = "budget_alert"
		simulatedAction["notification_channels"] = actions.Alert.NotificationChannels
		simulatedAction["threshold_type"] = actions.Alert.ThresholdType
		simulatedAction["threshold_value"] = actions.Alert.ThresholdValue

	case domain.RuleTypeCategoryRollover:
		if actions.Rollover == nil {
			return nil, fmt.Errorf("rollover action not defined")
		}
		remaining := 0.0
		if r, ok := triggerData["remaining"].(float64); ok {
			remaining = r
		}
		amount := s.calculateRolloverAmount(actions.Rollover, remaining)
		simulatedAction["action_type"] = "category_rollover"
		simulatedAction["rollover_type"] = actions.Rollover.RolloverType
		simulatedAction["rollover_amount"] = amount
		if actions.Rollover.SourceBudgetCategoryID != "" {
			simulatedAction["source_category_id"] = actions.Rollover.SourceBudgetCategoryID
		}

	case domain.RuleTypeSavingsGoal:
		if actions.SavingsGoal == nil {
			return nil, fmt.Errorf("savings goal action not defined")
		}
		amount := s.calculateSavingsGoalAmount(actions.SavingsGoal, triggerData)
		simulatedAction["action_type"] = "savings_goal"
		simulatedAction["savings_goal_id"] = actions.SavingsGoal.SavingsGoalID
		simulatedAction["from_account_id"] = actions.SavingsGoal.FromAccountID
		simulatedAction["amount"] = amount
		simulatedAction["amount_type"] = actions.SavingsGoal.AmountType

	default:
		return nil, fmt.Errorf("unknown rule type: %s", rule.RuleType)
	}

	return simulatedAction, nil
}

// executeTransferSuggestion creates a transfer suggestion from the rule action.
func (s *ruleExecutionService) executeTransferSuggestion(ctx context.Context, rule *domain.AutomationRule, executionID string, triggerData map[string]interface{}) error {
	// Parse actions
	var actions RuleActions
	if err := json.Unmarshal(rule.Actions, &actions); err != nil {
		return fmt.Errorf("failed to parse actions: %w", err)
	}

	if actions.Transfer == nil {
		return fmt.Errorf("transfer action not defined")
	}

	transfer := actions.Transfer

	// Calculate amount
	amount := s.calculateTransferAmount(transfer, triggerData)

	// Create reason string
	reason := fmt.Sprintf("Rule '%s' triggered", rule.Name)

	// Create suggestion
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          rule.UserID,
		RuleID:          ptrString(rule.ID),
		RuleExecutionID: ptrString(executionID),
		FromAccountID:   ptrString(transfer.FromAccountID),
		ToAccountID:     ptrString(transfer.ToAccountID),
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
	}

	// Issue #1109: Resolve stable user_account_id to current active account_id when available
	// (backfilled by migration 000070). This ensures suggestions point to live accounts after re-links.
	if transfer.FromUserAccountID != "" {
		suggestion.FromUserAccountID = ptrString(transfer.FromUserAccountID)
		if s.chainResolver != nil {
			if activeFromID, err := s.chainResolver.ResolveActiveID(ctx, transfer.FromUserAccountID); err == nil {
				suggestion.FromAccountID = ptrString(activeFromID)
			}
		}
	}
	if transfer.ToUserAccountID != "" {
		suggestion.ToUserAccountID = ptrString(transfer.ToUserAccountID)
		if s.chainResolver != nil {
			if activeToID, err := s.chainResolver.ResolveActiveID(ctx, transfer.ToUserAccountID); err == nil {
				suggestion.ToAccountID = ptrString(activeToID)
			}
		}
	}
	if transfer.FromUserAccountID != "" || transfer.ToUserAccountID != "" {
		observability.AccountsReplacedFiltered.WithLabelValues("automation").Inc()
	}

	if err := s.suggestionRepo.Create(ctx, suggestion); err != nil {
		return err
	}

	s.createTransferSuggestionNotification(ctx, suggestion)
	return nil
}

// createTransferSuggestionNotification creates an accept_reject notification for a new transfer suggestion.
// This is non-blocking: errors are logged and ignored so a notification failure never blocks rule execution.
func (s *ruleExecutionService) createTransferSuggestionNotification(ctx context.Context, suggestion *domain.TransferSuggestion) {
	if s.notificationRepo == nil {
		return
	}

	message := ""
	if suggestion.Reason != nil {
		message = *suggestion.Reason
	}

	suggestionID := suggestion.ID
	notification := &domain.Notification{
		UserID:               suggestion.UserID,
		Type:                 "transfer_suggestion",
		Title:                "Transfer Suggestion",
		Message:              message,
		InteractionPattern:   "accept_reject",
		TransferSuggestionID: &suggestionID,
		Read:                 false,
		SentEmail:            false,
	}

	if err := s.notificationRepo.Create(ctx, notification); err != nil {
		slog.Warn("failed to create transfer suggestion notification",
			"suggestion_id", suggestion.ID,
			"error", err,
		)
	}
}

// calculateTransferAmount calculates the transfer amount based on action type.
func (s *ruleExecutionService) calculateTransferAmount(action *domain.TransferAction, triggerData map[string]interface{}) float64 {
	switch action.AmountType {
	case "fixed":
		return action.Amount
	case "percentage":
		if amt, ok := triggerData["amount"].(float64); ok {
			return amt * (action.Amount / 100)
		}
	case "remaining":
		if remaining, ok := triggerData["remaining"].(float64); ok {
			return remaining
		}
	case "threshold":
		if amt, ok := triggerData["amount"].(float64); ok {
			if amt > action.Threshold {
				return amt - action.Threshold
			}
		}
	}
	return action.Amount
}

// executeBudgetAlert sends a budget alert notification.
func (s *ruleExecutionService) executeBudgetAlert(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) error {
	// Parse actions
	var actions RuleActions
	if err := json.Unmarshal(rule.Actions, &actions); err != nil {
		return fmt.Errorf("failed to parse actions: %w", err)
	}

	if actions.Alert == nil {
		return fmt.Errorf("alert action not defined")
	}

	// Send notification if service is available
	if s.notificationService != nil {
		return s.notificationService.SendBudgetAlert(ctx, rule.UserID, actions.Alert, triggerData)
	}

	// If no notification service, just return success
	// The alert was "executed" by evaluating the condition
	return nil
}

// executeCategoryRollover creates a rollover suggestion.
func (s *ruleExecutionService) executeCategoryRollover(ctx context.Context, rule *domain.AutomationRule, executionID string, triggerData map[string]interface{}) error {
	// Parse actions
	var actions RuleActions
	if err := json.Unmarshal(rule.Actions, &actions); err != nil {
		return fmt.Errorf("failed to parse actions: %w", err)
	}

	if actions.Rollover == nil {
		return fmt.Errorf("rollover action not defined")
	}

	rollover := actions.Rollover

	// Calculate rollover amount from trigger data
	remaining := 0.0
	if r, ok := triggerData["remaining"].(float64); ok {
		remaining = r
	}

	amount := s.calculateRolloverAmount(rollover, remaining)
	if amount <= 0 {
		// Nothing to rollover
		return nil
	}

	// Create reason string
	reason := fmt.Sprintf("Category rollover from rule '%s'", rule.Name)

	// Create suggestion for the rollover
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          rule.UserID,
		RuleID:          ptrString(rule.ID),
		RuleExecutionID: ptrString(executionID),
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
	}

	if rollover.SourceBudgetCategoryID != "" {
		suggestion.FromAccountID = ptrString(rollover.SourceBudgetCategoryID)
	}

	if err := s.suggestionRepo.Create(ctx, suggestion); err != nil {
		return err
	}

	s.createTransferSuggestionNotification(ctx, suggestion)
	return nil
}

// calculateRolloverAmount calculates the rollover amount based on rollover type.
func (s *ruleExecutionService) calculateRolloverAmount(action *domain.RolloverAction, remaining float64) float64 {
	if remaining <= 0 {
		return 0
	}

	switch action.RolloverType {
	case "all", "full":
		return remaining
	case "capped":
		if action.CapAmount != nil && remaining > *action.CapAmount {
			return *action.CapAmount
		}
		return remaining
	case "percentage":
		if action.Percentage != nil {
			return remaining * (*action.Percentage / 100)
		}
		return remaining
	}
	return remaining
}

// executeSavingsGoal creates a transfer suggestion to contribute to a savings goal.
// Issue #36: Rule Executors - Savings Goal Executor.
func (s *ruleExecutionService) executeSavingsGoal(ctx context.Context, rule *domain.AutomationRule, executionID string, triggerData map[string]interface{}) error {
	// Parse actions
	var actions RuleActions
	if err := json.Unmarshal(rule.Actions, &actions); err != nil {
		return fmt.Errorf("failed to parse actions: %w", err)
	}

	if actions.SavingsGoal == nil {
		return fmt.Errorf("savings goal action not defined")
	}

	savingsGoalAction := actions.SavingsGoal

	// Get the savings goal to find the linked account
	if s.savingsGoalRepo == nil {
		return fmt.Errorf("savings goal repository not configured")
	}

	goal, err := s.savingsGoalRepo.GetByID(ctx, savingsGoalAction.SavingsGoalID)
	if err != nil {
		return fmt.Errorf("savings goal not found: %w", err)
	}

	// Calculate the contribution amount
	amount := s.calculateSavingsGoalAmount(savingsGoalAction, triggerData)
	if amount <= 0 {
		// Nothing to transfer
		return nil
	}

	// Create reason string
	reason := fmt.Sprintf("Savings contribution to '%s' from rule '%s'", goal.Name, rule.Name)

	// Issue #1109: Resolve savings goal's account to current active account_id via stable user_account_id
	toAccountID := goal.AccountID
	if goal.UserAccountID != nil && *goal.UserAccountID != "" && s.chainResolver != nil {
		if activeID, err := s.chainResolver.ResolveActiveID(ctx, *goal.UserAccountID); err == nil {
			toAccountID = activeID
		}
	}

	// Create transfer suggestion from source account to savings goal's linked account
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          rule.UserID,
		RuleID:          ptrString(rule.ID),
		RuleExecutionID: ptrString(executionID),
		FromAccountID:   ptrString(savingsGoalAction.FromAccountID),
		ToAccountID:     ptrString(toAccountID), // Resolved to active account (Issue #1109)
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
	}

	if err := s.suggestionRepo.Create(ctx, suggestion); err != nil {
		return err
	}

	s.createTransferSuggestionNotification(ctx, suggestion)
	return nil
}

// calculateSavingsGoalAmount calculates the contribution amount based on action type.
func (s *ruleExecutionService) calculateSavingsGoalAmount(action *domain.SavingsGoalAction, triggerData map[string]interface{}) float64 {
	switch action.AmountType {
	case "fixed":
		return action.Amount
	case "percentage":
		if action.Percentage != nil {
			if amt, ok := triggerData["amount"].(float64); ok {
				return amt * (*action.Percentage / 100)
			}
		}
		return 0
	case "remaining":
		// Calculate remaining amount above threshold
		if balance, ok := triggerData["balance"].(float64); ok {
			remaining := balance - action.Threshold
			if remaining > 0 {
				return remaining
			}
		}
		return 0
	}
	return action.Amount
}

// evaluateConditions evaluates all conditions with the given logic.
func evaluateConditions(conditions []domain.AutomationRuleCondition, logic string, data map[string]interface{}) bool {
	if len(conditions) == 0 {
		return true
	}

	if logic == "OR" {
		for _, c := range conditions {
			if evaluateCondition(c, data) {
				return true
			}
		}
		return false
	}

	// Default to AND logic
	for _, c := range conditions {
		if !evaluateCondition(c, data) {
			return false
		}
	}
	return true
}

// evaluateCondition evaluates a single condition against the data.
// Supports both short form (eq, neq, gt, gte, lt, lte) and long form
// (equals, not_equals, greater_than, etc.) operator names.
func evaluateCondition(condition domain.AutomationRuleCondition, data map[string]interface{}) bool {
	fieldValue, exists := data[condition.Field]
	if !exists {
		return false
	}

	conditionValue := fmt.Sprintf("%v", condition.Value)
	op := condition.Operator

	switch op {
	case domain.ConditionOperatorEq, "equals":
		return fmt.Sprintf("%v", fieldValue) == conditionValue
	case domain.ConditionOperatorNeq, "not_equals":
		return fmt.Sprintf("%v", fieldValue) != conditionValue
	case domain.ConditionOperatorContains, "not_contains":
		if op == "not_contains" {
			return !strings.Contains(fmt.Sprintf("%v", fieldValue), conditionValue)
		}
		return strings.Contains(fmt.Sprintf("%v", fieldValue), conditionValue)
	case "starts_with":
		return strings.HasPrefix(fmt.Sprintf("%v", fieldValue), conditionValue)
	case "ends_with":
		return strings.HasSuffix(fmt.Sprintf("%v", fieldValue), conditionValue)
	case domain.ConditionOperatorGt, "greater_than":
		return compareNumeric(fieldValue, conditionValue, ">")
	case domain.ConditionOperatorLt, "less_than":
		return compareNumeric(fieldValue, conditionValue, "<")
	case domain.ConditionOperatorGte, "greater_than_or_equal":
		return compareNumeric(fieldValue, conditionValue, ">=")
	case domain.ConditionOperatorLte, "less_than_or_equal":
		return compareNumeric(fieldValue, conditionValue, "<=")
	case "in":
		return containsValue(conditionValue, fmt.Sprintf("%v", fieldValue))
	case "not_in":
		return !containsValue(conditionValue, fmt.Sprintf("%v", fieldValue))
	default:
		return false
	}
}

// compareNumeric compares two values numerically.
func compareNumeric(fieldValue interface{}, conditionValue string, operator string) bool {
	var fieldNum float64

	switch v := fieldValue.(type) {
	case float64:
		fieldNum = v
	case float32:
		fieldNum = float64(v)
	case int:
		fieldNum = float64(v)
	case int64:
		fieldNum = float64(v)
	case string:
		var err error
		fieldNum, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return false
		}
	default:
		return false
	}

	condNum, err := strconv.ParseFloat(conditionValue, 64)
	if err != nil {
		return false
	}

	switch operator {
	case ">":
		return fieldNum > condNum
	case "<":
		return fieldNum < condNum
	case ">=":
		return fieldNum >= condNum
	case "<=":
		return fieldNum <= condNum
	default:
		return false
	}
}

// containsValue checks if a comma-separated list contains a value.
func containsValue(list, value string) bool {
	values := strings.Split(list, ",")
	for _, v := range values {
		if strings.TrimSpace(v) == value {
			return true
		}
	}
	return false
}

// Helper functions for optional pointers.
func ptrTime(t time.Time) *time.Time {
	return &t
}

func ptrString(s string) *string {
	return &s
}
