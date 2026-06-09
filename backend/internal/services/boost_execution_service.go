// Package services provides business logic for boost execution.
// Issue #35: Rules Engine Core - condition evaluation and action execution.
package services

import (
	"context"
	"encoding/json"
	"errors"
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

// ExecutionStatusSkipped is a synthetic status for skipped executions.
// In the database, this maps to "completed" with output_data containing skip_reason.
// This allows us to track skipped executions separately in the service layer.
const ExecutionStatusSkipped domain.ExecutionStatus = "completed"

// BoostExecutionService defines the interface for boost execution business logic.
type BoostExecutionService interface {
	// ExecuteBoost executes a single rule with the given trigger data.
	ExecuteBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error)

	// ProcessEventTrigger processes an event trigger for all matching rules.
	ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error

	// GetBoostExecutionHistory retrieves execution history for a rule.
	GetBoostExecutionHistory(ctx context.Context, boostID string, limit, offset int) ([]*domain.BoostExecution, error)

	// GetExecutionHistoryWithFilter retrieves execution history with optional status filter and total count.
	// Issue #38: Rules API Endpoints - Execution history with filtering.
	GetExecutionHistoryWithFilter(ctx context.Context, boostID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error)

	// TestBoost performs a dry-run execution of a rule without persisting any changes.
	// Issue #38: Rules API Endpoints - Test endpoint.
	TestBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostTestResult, error)
}

// boostExecutionService implements BoostExecutionService.
type boostExecutionService struct {
	executionRepo       repository.BoostExecutionRepository
	suggestionRepo      repository.TransferSuggestionRepository
	ruleRepo            repository.BoostsRepository
	savingsGoalRepo     repository.SavingsGoalRepository
	accountRepo         repository.AccountRepository  // optional; for execution-time ownership checks (GAP AE)
	notificationService NotificationService
	notificationRepo    repository.NotificationRepository
	analytics           phclient.Analytics
	chainResolver       AccountChainResolver // Issue #1109: resolves active account IDs across re-links
}

// NotificationService defines the interface for sending notifications.
type NotificationService interface {
	SendBudgetAlert(ctx context.Context, userID string, alert *domain.AlertAction, data map[string]interface{}) error
}

// NewBoostExecutionService creates a new BoostExecutionService instance.
func NewBoostExecutionService(
	executionRepo repository.BoostExecutionRepository,
	suggestionRepo repository.TransferSuggestionRepository,
	ruleRepo repository.BoostsRepository,
	notificationService NotificationService,
) *boostExecutionService {
	return &boostExecutionService{
		executionRepo:       executionRepo,
		suggestionRepo:      suggestionRepo,
		ruleRepo:            ruleRepo,
		notificationService: notificationService,
	}
}

// SetAnalytics sets the analytics client for event capture.
func (s *boostExecutionService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// SetSavingsGoalRepo sets the savings goal repository for savings goal executor.
func (s *boostExecutionService) SetSavingsGoalRepo(repo repository.SavingsGoalRepository) {
	s.savingsGoalRepo = repo
}

// SetNotificationRepo sets the notification repository for creating transfer suggestion notifications.
func (s *boostExecutionService) SetNotificationRepo(repo repository.NotificationRepository) {
	s.notificationRepo = repo
}

// SetAccountRepo injects the account repository for execution-time ownership checks.
// When set, executeTransferSuggestion verifies from/to_account_id belong to the rule owner.
// Issue #972: GAP AE defense-in-depth for already-persisted transfer rules.
func (s *boostExecutionService) SetAccountRepo(repo repository.AccountRepository) {
	s.accountRepo = repo
}

// SetChainResolver injects the chain resolver to map user_account_id → active account_id.
// Issue #1109: Used by executeTransferSuggestion and executeSavingsGoal.
func (s *boostExecutionService) SetChainResolver(resolver AccountChainResolver) {
	s.chainResolver = resolver
}

// ExecuteBoost executes a single rule with the given trigger data.
func (s *boostExecutionService) ExecuteBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
	// Defense-in-depth: re-validate action completeness before dispatching.
	// The service layer validates on create/update, but rules loaded directly from the
	// DB or injected in tests may bypass that path. Re-validating here prevents a
	// partial-field action (e.g. transfer without to_account_id) from creating a
	// malformed suggestion. Cost: one cheap in-memory validation per execution.
	if err := boost.Validate(); err != nil {
		return nil, fmt.Errorf("boost validation failed: %w", err)
	}

	startTime := time.Now()

	// Create execution record
	triggerContextJSON, _ := json.Marshal(triggerData)
	execution := &domain.BoostExecution{
		ID:             uuid.New().String(),
		BoostID: boost.ID,
		Status:         domain.ExecutionStatusRunning,
		StartedAt:      startTime,
		TriggerContext: triggerContextJSON,
	}

	// Save initial execution record
	if err := s.executionRepo.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution record: %w", err)
	}

	// Check if rule is enabled
	if !boost.Enabled {
		s.markExecutionSkipped(execution, startTime, "boost disabled")
		_ = s.executionRepo.Update(ctx, execution)
		return execution, nil
	}

	// Evaluate conditions
	if !evaluateConditions(boost.Conditions, boost.ConditionLogic, triggerData) {
		s.markExecutionSkipped(execution, startTime, "conditions not met")
		_ = s.executionRepo.Update(ctx, execution)
		return execution, nil
	}

	// Execute action based on rule type
	var actionErr error
	switch boost.BoostType {
	case domain.BoostTypeTransferSuggestion:
		actionErr = s.executeTransferSuggestion(ctx, boost, execution.ID, triggerData)
	case domain.BoostTypeBudgetAlert:
		actionErr = s.executeBudgetAlert(ctx, boost, triggerData)
	case domain.BoostTypeCategoryRollover:
		actionErr = s.executeCategoryRollover(ctx, boost, execution.ID, triggerData)
	case domain.BoostTypeSavingsGoal:
		actionErr = s.executeSavingsGoal(ctx, boost, execution.ID, triggerData)
	default:
		actionErr = fmt.Errorf("unknown boost type: %s", boost.BoostType)
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
	_ = s.ruleRepo.IncrementExecutionCount(ctx, boost.ID)
	_ = s.ruleRepo.UpdateLastSuccessful(ctx, boost.ID)

	if s.analytics != nil {
		s.analytics.Capture(boost.UserID, phclient.EventBoostTriggered, map[string]interface{}{
			"rule_id":   boost.ID,
			"rule_type": string(boost.BoostType),
		})
	}

	return execution, nil
}

// ProcessEventTrigger processes an event trigger for all matching rules.
// Simplified: No tier restrictions on event-based triggers.
func (s *boostExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	// Get matching rules for this event
	rules, err := s.ruleRepo.GetBoostsByEventTrigger(ctx, userID, eventType)
	if err != nil {
		return err
	}

	// Execute each matching rule. Errors are logged but do not abort the batch —
	// one rule failing (e.g. transient DB error, missing repo) must not prevent
	// other rules for the same user from firing on the same event.
	eventBudgetID, _ := eventData[domain.EventDataKeyBudgetID].(string)
	for _, boost := range rules {
		// A-CR2: if the rule is scoped to a specific budget (via budget_category.* conditions),
		// skip it entirely when the event belongs to a different budget. This closes the OR-escape
		// bug where an OR-logic rule could fire via a non-category condition even though its
		// budget_category condition is for a different budget.
		if boundID, bound := boost.BoundBudgetID(); bound && boundID != eventBudgetID {
			continue
		}
		_, execErr := s.ExecuteBoost(ctx, boost, eventData)
		if execErr != nil {
			slog.Error("boost execution failed; continuing with remaining rules",
				"rule_id", boost.ID,
				"user_id", boost.UserID,
				"event_type", eventType,
				"error", execErr,
			)
		}
	}

	return nil
}

// GetBoostExecutionHistory retrieves execution history for a rule.
func (s *boostExecutionService) GetBoostExecutionHistory(ctx context.Context, boostID string, limit, offset int) ([]*domain.BoostExecution, error) {
	return s.executionRepo.GetByBoostID(ctx, boostID, limit, offset)
}

// GetExecutionHistoryWithFilter retrieves execution history with optional status filter and total count.
// Issue #38: Rules API Endpoints - Execution history with filtering.
func (s *boostExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, boostID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	return s.executionRepo.GetByBoostIDWithFilter(ctx, boostID, status, limit, offset)
}

// TestBoost performs a dry-run execution of a rule without persisting any changes.
// Issue #38: Rules API Endpoints - Test endpoint.
// This method:
// - Evaluates each condition and returns individual results
// - Simulates what action would occur (without persisting)
// - Works on disabled rules for preview purposes
// - Does NOT count toward daily execution limits
// - Does NOT persist execution records
// - Does NOT create transfer suggestions or send notifications
func (s *boostExecutionService) TestBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostTestResult, error) {
	result := &domain.BoostTestResult{
		BoostID: boost.ID,
		BoostName: boost.Name,
		BoostType: boost.BoostType,
		TriggerDataUsed: triggerData,
	}

	// Evaluate each condition individually.
	// Use lookupFieldValue for the Actual display so that budget_category.* conditions
	// read from the nested categories map, and other fields resolve via
	// normalizeConditionField. Fall back to the raw dotted field key for emitters
	// (e.g. transaction events) that publish the full dotted name as-is.
	conditionResults := make([]domain.ConditionEvalResult, len(boost.Conditions))
	for i, condition := range boost.Conditions {
		actual, exists := lookupFieldValue(condition, triggerData)
		if !exists && !strings.HasPrefix(condition.Field, "budget_category.") {
			// Fallback: some emitters publish the dotted key verbatim (e.g.
			// "transaction.pfc_primary") without stripping the entity prefix.
			actual, exists = triggerData[condition.Field]
		}
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
	result.ConditionsMet = evaluateConditions(boost.Conditions, boost.ConditionLogic, triggerData)

	// If conditions are met, simulate the action without persisting
	if result.ConditionsMet {
		simulatedAction, err := s.simulateAction(boost, triggerData)
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
func (s *boostExecutionService) simulateAction(boost *domain.Boost, triggerData map[string]interface{}) (map[string]interface{}, error) {
	simulatedAction := make(map[string]interface{})

	switch boost.BoostType {
	case domain.BoostTypeTransferSuggestion:
		transfer, err := boost.ParseTransferAction()
		if err != nil {
			return nil, fmt.Errorf("failed to parse transfer action: %w", err)
		}
		if transfer.FromAccountID == "" {
			return nil, fmt.Errorf("transfer action not defined")
		}
		amount := s.calculateTransferAmount(transfer, triggerData)
		simulatedAction["action_type"] = "transfer_suggestion"
		simulatedAction["from_account_id"] = transfer.FromAccountID
		simulatedAction["to_account_id"] = transfer.ToAccountID
		simulatedAction["amount"] = amount
		simulatedAction["amount_type"] = transfer.AmountType
		// Issue #1109: Expose stable user_account_id for transparency if available
		if transfer.FromUserAccountID != "" {
			simulatedAction["from_user_account_id"] = transfer.FromUserAccountID
		}

	case domain.BoostTypeBudgetAlert:
		alert, err := boost.ParseAlertAction()
		if err != nil {
			return nil, fmt.Errorf("failed to parse alert action: %w", err)
		}
		if alert.ThresholdType == "" {
			return nil, fmt.Errorf("alert action not defined")
		}
		simulatedAction["action_type"] = "budget_alert"
		simulatedAction["notification_channels"] = alert.NotificationChannels
		simulatedAction["threshold_type"] = alert.ThresholdType
		simulatedAction["threshold_value"] = alert.ThresholdValue

	case domain.BoostTypeCategoryRollover:
		rollover, err := boost.ParseRolloverAction()
		if err != nil {
			return nil, fmt.Errorf("failed to parse rollover action: %w", err)
		}
		if rollover.RolloverType == "" {
			return nil, fmt.Errorf("rollover action not defined")
		}
		remaining := 0.0
		if r, ok := triggerData["remaining"].(float64); ok {
			remaining = r
		}
		amount := s.calculateRolloverAmount(rollover, remaining)
		simulatedAction["action_type"] = "category_rollover"
		simulatedAction["rollover_type"] = rollover.RolloverType
		simulatedAction["rollover_amount"] = amount
		if rollover.SourceBudgetCategoryID != "" {
			simulatedAction["source_category_id"] = rollover.SourceBudgetCategoryID
		}

	case domain.BoostTypeSavingsGoal:
		savingsGoal, err := boost.ParseSavingsGoalAction()
		if err != nil {
			return nil, fmt.Errorf("failed to parse savings goal action: %w", err)
		}
		if savingsGoal.SavingsGoalID == "" {
			return nil, fmt.Errorf("savings goal action not defined")
		}
		amount := s.calculateSavingsGoalAmount(savingsGoal, triggerData)
		simulatedAction["action_type"] = "savings_goal"
		simulatedAction["savings_goal_id"] = savingsGoal.SavingsGoalID
		simulatedAction["from_account_id"] = savingsGoal.FromAccountID
		simulatedAction["amount"] = amount
		simulatedAction["amount_type"] = savingsGoal.AmountType

	default:
		return nil, fmt.Errorf("unknown boost type: %s", boost.BoostType)
	}

	return simulatedAction, nil
}

// executeTransferSuggestion creates a transfer suggestion from the rule action.
func (s *boostExecutionService) executeTransferSuggestion(ctx context.Context, boost *domain.Boost, executionID string, triggerData map[string]interface{}) error {
	transfer, err := boost.ParseTransferAction()
	if err != nil {
		return fmt.Errorf("failed to parse transfer action: %w", err)
	}
	if transfer.FromAccountID == "" {
		return fmt.Errorf("transfer action not defined")
	}

	// Execution-time ownership check (GAP AE defense-in-depth).
	// The service layer rejects new rules with mismatched IDs; this guard protects
	// already-persisted rules that may predate that check.
	if s.accountRepo != nil {
		if _, err := s.accountRepo.GetByIDAndUser(ctx, transfer.FromAccountID, boost.UserID); err != nil {
			slog.Warn("transfer executor: from_account_id ownership mismatch; skipping boost",
				"rule_id", boost.ID, "user_id", boost.UserID, "from_account_id", transfer.FromAccountID)
			return nil
		}
		if transfer.ToAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, transfer.ToAccountID, boost.UserID); err != nil {
				slog.Warn("transfer executor: to_account_id ownership mismatch; skipping boost",
					"rule_id", boost.ID, "user_id", boost.UserID, "to_account_id", transfer.ToAccountID)
				return nil
			}
		}
	}

	// Calculate amount; skip if zero (no surplus to transfer for derived types).
	amount := s.calculateTransferAmount(transfer, triggerData)
	if amount <= 0 {
		return nil
	}

	// Create reason string
	reason := fmt.Sprintf("Rule '%s' triggered", boost.Name)

	// Create suggestion
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour) // 30 days
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          boost.UserID,
		BoostID: ptrString(boost.ID),
		BoostExecutionID: ptrString(executionID),
		FromAccountID:   ptrString(transfer.FromAccountID),
		ToAccountID:     ptrString(transfer.ToAccountID),
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceBoost,
		Status:          domain.SuggestionStatusPending,
		ExpiresAt:       &expiresAt,
	}

	// Issue #1109: Resolve stable user_account_id → current active account_id (backfilled by migration 000070).
	// Issue #972 GAP AE: after resolution, verify the resolved account still belongs to rule.UserID.
	// Without this check an attacker could provide their own from_account_id (passes the guard above)
	// plus a victim's from_user_account_id, and the unscoped resolver would overwrite FromAccountID
	// with the victim's active account. The ownership check is the backstop.
	if transfer.FromUserAccountID != "" {
		suggestion.FromUserAccountID = ptrString(transfer.FromUserAccountID)
		if skipRule, _ := s.resolveAndVerifyUserAccount(ctx, transfer.FromUserAccountID, boost.UserID, boost.ID,
			func(activeID string) { suggestion.FromAccountID = ptrString(activeID) }, "from_account_id"); skipRule {
			return nil
		}
	}
	if transfer.ToUserAccountID != "" {
		suggestion.ToUserAccountID = ptrString(transfer.ToUserAccountID)
		if skipRule, _ := s.resolveAndVerifyUserAccount(ctx, transfer.ToUserAccountID, boost.UserID, boost.ID,
			func(activeID string) { suggestion.ToAccountID = ptrString(activeID) }, "to_account_id"); skipRule {
			return nil
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

// resolveAndVerifyUserAccount resolves a user_account_id to its active account_id and verifies ownership.
// Called during transfer suggestion execution to resolve stable identifiers across Plaid re-links.
// If resolution succeeds, calls onResolve with the active ID. Returns (true, nil) if ownership fails
// and the entire rule should be skipped; otherwise returns (false, nil).
func (s *boostExecutionService) resolveAndVerifyUserAccount(ctx context.Context, userAccountID, userID, boostID string,
	onResolve func(string), fieldName string) (bool, error) {
	if s.chainResolver == nil {
		return false, nil
	}

	activeID, err := s.chainResolver.ResolveActiveID(ctx, userAccountID)
	if err != nil {
		if !errors.Is(err, ErrNoActiveAccount) {
			slog.Warn("chain resolve failed; using raw id", "error", err, "field", fieldName)
		}
		return false, nil
	}

	// Verify the resolved account belongs to this user before using it.
	// If accountRepo is not configured, apply the resolved ID without an ownership check
	// and log a warning — this is safe because (a) production always wires accountRepo
	// (main.go:~1324) and (b) the service-layer validateRuleReferenceOwnership already
	// validates from_user_account_id ownership at create/update time. Issue #972 GAP AE.
	if s.accountRepo != nil {
		if _, ownerErr := s.accountRepo.GetByIDAndUser(ctx, activeID, userID); ownerErr != nil {
			slog.Warn("chain resolve: resolved account belongs to different user; skipping boost",
				"rule_id", boostID, "user_id", userID, "resolved_account_id", activeID, "field", fieldName)
			return true, nil
		}
	} else {
		slog.Warn("chain resolve: accountRepo not configured; applying resolved ID without ownership check (configure SetAccountRepo in production)",
			"rule_id", boostID, "user_id", userID, "field", fieldName)
	}
	onResolve(activeID)
	return false, nil
}

// createTransferSuggestionNotification creates an accept_reject notification for a new transfer suggestion.
// This is non-blocking: errors are logged and ignored so a notification failure never blocks rule execution.
func (s *boostExecutionService) createTransferSuggestionNotification(ctx context.Context, suggestion *domain.TransferSuggestion) {
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

// calculateTransferAmount computes the transfer amount based on action type:
// fixed: Amount directly; percentage: Amount% of budget income; remaining: full surplus;
// excess: surplus above Threshold; threshold: legacy (use excess).
func (s *boostExecutionService) calculateTransferAmount(action *domain.TransferAction, triggerData map[string]interface{}) float64 {
	if action.AmountType == "fixed" {
		return action.Amount
	}

	if action.AmountType == "percentage" {
		if amt, ok := triggerData[domain.EventDataKeyAmount].(float64); ok {
			return amt * (action.Amount / 100)
		}
		return 0
	}

	if action.AmountType == "remaining" {
		if remaining, ok := triggerData[domain.EventDataKeyRemaining].(float64); ok {
			return remaining
		}
		// Key absent (e.g. schedule trigger has no budget surplus data): no transfer.
		return 0
	}

	if action.AmountType == "excess" {
		remaining, ok := triggerData[domain.EventDataKeyRemaining].(float64)
		if !ok {
			return 0 // Key absent: no transfer (GAP AF)
		}
		if remaining > action.Threshold {
			return remaining - action.Threshold
		}
		return 0 // Surplus below threshold: no transfer
	}

	if action.AmountType == "threshold" {
		amt, ok := triggerData[domain.EventDataKeyAmount].(float64)
		if !ok {
			return 0 // Key absent: no transfer (GAP AF)
		}
		if amt > action.Threshold {
			return amt - action.Threshold
		}
		return 0 // Amount does not exceed threshold: no transfer
	}

	// Unrecognized type defaults to fixed
	return action.Amount
}

// executeBudgetAlert sends a budget alert notification.
func (s *boostExecutionService) executeBudgetAlert(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) error {
	alert, err := boost.ParseAlertAction()
	if err != nil {
		return fmt.Errorf("failed to parse alert action: %w", err)
	}
	if alert.ThresholdType == "" {
		return fmt.Errorf("alert action not defined")
	}

	// Send notification if service is available
	if s.notificationService != nil {
		return s.notificationService.SendBudgetAlert(ctx, boost.UserID, alert, triggerData)
	}

	// If no notification service, just return success
	// The alert was "executed" by evaluating the condition
	return nil
}

// executeCategoryRollover creates a rollover suggestion.
func (s *boostExecutionService) executeCategoryRollover(ctx context.Context, boost *domain.Boost, executionID string, triggerData map[string]interface{}) error {
	rollover, err := boost.ParseRolloverAction()
	if err != nil {
		return fmt.Errorf("failed to parse rollover action: %w", err)
	}
	if rollover.RolloverType == "" {
		return fmt.Errorf("rollover action not defined")
	}

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
	reason := fmt.Sprintf("Category rollover from boost '%s'", boost.Name)

	// Create suggestion for the rollover
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          boost.UserID,
		BoostID: ptrString(boost.ID),
		BoostExecutionID: ptrString(executionID),
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceBoost,
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
func (s *boostExecutionService) calculateRolloverAmount(action *domain.RolloverAction, remaining float64) float64 {
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
func (s *boostExecutionService) executeSavingsGoal(ctx context.Context, boost *domain.Boost, executionID string, triggerData map[string]interface{}) error {
	savingsGoalAction, err := boost.ParseSavingsGoalAction()
	if err != nil {
		return fmt.Errorf("failed to parse savings goal action: %w", err)
	}
	if savingsGoalAction.SavingsGoalID == "" {
		return fmt.Errorf("savings goal action not defined")
	}

	// Get the savings goal to find the linked account, enforcing ownership (GAP AE).
	// GetByIDForUser returns ErrSavingsGoalNotFound if the goal belongs to a different user,
	// preventing IDOR for already-persisted rules that bypassed service-layer validation.
	if s.savingsGoalRepo == nil {
		return fmt.Errorf("savings goal repository not configured")
	}

	goal, err := s.savingsGoalRepo.GetByIDForUser(ctx, savingsGoalAction.SavingsGoalID, boost.UserID)
	if err != nil {
		// Log but don't fail the batch — this rule is silently skipped.
		slog.Warn("savings goal executor: goal not found or ownership mismatch; skipping boost",
			"rule_id", boost.ID, "user_id", boost.UserID,
			"savings_goal_id", savingsGoalAction.SavingsGoalID, "error", err)
		return nil
	}

	// Calculate the contribution amount
	amount := s.calculateSavingsGoalAmount(savingsGoalAction, triggerData)
	if amount <= 0 {
		// Nothing to transfer
		return nil
	}

	// Create reason string
	reason := fmt.Sprintf("Savings contribution to '%s' from boost '%s'", goal.Name, boost.Name)

	// Issue #1109: Resolve savings goal's account to current active account_id via stable user_account_id.
	toAccountID := goal.AccountID
	if goal.UserAccountID != nil && *goal.UserAccountID != "" && s.chainResolver != nil {
		if activeID, err := s.chainResolver.ResolveActiveID(ctx, *goal.UserAccountID); err == nil {
			toAccountID = activeID
			observability.AccountsReplacedFiltered.WithLabelValues("automation").Inc()
		} else if !errors.Is(err, ErrNoActiveAccount) {
			slog.Warn("chain resolve failed for savings goal account; using raw id", "error", err)
		}
	}

	// Create transfer suggestion from source account to savings goal's linked account.
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour) // 30 days
	suggestion := &domain.TransferSuggestion{
		ID:              uuid.New().String(),
		UserID:          boost.UserID,
		BoostID: ptrString(boost.ID),
		BoostExecutionID: ptrString(executionID),
		FromAccountID:   ptrString(savingsGoalAction.FromAccountID),
		ToAccountID:     ptrString(toAccountID),
		Amount:          amount,
		CurrencyCode:    "USD",
		Reason:          &reason,
		Source:          domain.SuggestionSourceBoost,
		Status:          domain.SuggestionStatusPending,
		ExpiresAt:       &expiresAt,
	}
	// Propagate stable user_account_id so monitoring can re-resolve post-relink.
	if goal.UserAccountID != nil {
		suggestion.ToUserAccountID = goal.UserAccountID
	}

	if err := s.suggestionRepo.Create(ctx, suggestion); err != nil {
		return err
	}

	s.createTransferSuggestionNotification(ctx, suggestion)
	return nil
}

// calculateSavingsGoalAmount calculates the contribution amount based on action type.
func (s *boostExecutionService) calculateSavingsGoalAmount(action *domain.SavingsGoalAction, triggerData map[string]interface{}) float64 {
	if action.AmountType == "fixed" {
		return action.Amount
	}

	if action.AmountType == "percentage" && action.Percentage != nil {
		if amt, ok := triggerData[domain.EventDataKeyAmount].(float64); ok {
			return amt * (*action.Percentage / 100)
		}
		return 0
	}

	if action.AmountType == "remaining" {
		// EventData key must match the emitter in budget_period_close_check.go which
		// publishes "remaining" (not "balance"). See domain.EventDataKeyRemaining.
		if remainingAmt, ok := triggerData[domain.EventDataKeyRemaining].(float64); ok {
			contribution := remainingAmt - action.Threshold
			if contribution > 0 {
				return contribution
			}
		}
		return 0
	}

	// Unrecognized type defaults to fixed
	return action.Amount
}

// evaluateConditions evaluates all conditions with the given logic.
func evaluateConditions(conditions []domain.BoostCondition, logic string, data map[string]interface{}) bool {
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

// normalizeSuffix maps UI field suffixes to engine keys.
// Used by both normalizeConditionField and lookupFieldValue for consistency.
func normalizeSuffix(suffix string) string {
	switch suffix {
	case "remaining_amount":
		return "remaining"
	case "spent_amount":
		// Issue #1271 Item 5: emitter key renamed from "total_spent" to "spent"
		return "spent"
	default:
		return suffix
	}
}

// normalizeConditionField maps UI-built dotted field names (e.g. "budget.remaining_amount")
// to flat engine keys (e.g. "remaining"). The rule-builder stores entity-prefixed names,
// but event emitters publish flat keys. Without normalization, all UI-built conditions
// would silently fail.
//
// Mapping:
//   - Strip entity prefix: "budget_category.", "budget.", "account.", "transaction."
//     (longer prefixes checked first)
//   - Alias suffix changes via normalizeSuffix
//   - Unknown/flat fields returned as-is
func normalizeConditionField(field string) string {
	// Strip entity prefix (longer prefixes must be checked first)
	prefixes := []string{"budget_category.", "budget.", "account.", "transaction."}
	suffix := field
	for _, p := range prefixes {
		if strings.HasPrefix(field, p) {
			suffix = field[len(p):]
			break
		}
	}
	return normalizeSuffix(suffix)
}

// lookupFieldValue resolves the condition field to its value in data.
//
// For budget_category.* fields the value is read from the nested
// data["categories"][condition.CategoryID][suffix] structure that category
// emitters populate. Handles both the typed map[string]map[string]float64
// shape (from BuildCategoryEventData) and the map[string]interface{} shape
// that arrives after a River JSON round-trip.
// All other fields fall back to a flat lookup after normalizing the field name
// via normalizeConditionField.
// Issue #1270: GAP M — per-category condition evaluation.
// Issue #1271 Items 5+6: dual-shape reader, budget-scope guard.
func lookupFieldValue(condition domain.BoostCondition, data map[string]interface{}) (interface{}, bool) {
	if strings.HasPrefix(condition.Field, "budget_category.") {
		// Defense-in-depth budget scope check (primary guard is in ProcessEventTrigger).
		if condition.BudgetID != "" {
			if eventBudgetID, _ := data[domain.EventDataKeyBudgetID].(string); eventBudgetID != condition.BudgetID {
				return nil, false
			}
		}
		suffix := normalizeSuffix(condition.Field[len("budget_category."):])
		return lookupCategoryMetric(data[domain.EventDataKeyCategories], condition.CategoryID, suffix)
	}
	// Flat lookup with prefix stripping + suffix normalization.
	normalizedField := normalizeConditionField(condition.Field)
	val, ok := data[normalizedField]
	return val, ok
}

// lookupCategoryMetric extracts a metric value from a categories map for a given category
// and key suffix. Handles both the typed map[string]map[string]float64 shape emitted by
// BuildCategoryEventData and the map[string]interface{} shape that arrives after a River
// JSON round-trip (where numeric values become json.Number). Issue #1271 Item 6.
func lookupCategoryMetric(categoriesRaw interface{}, categoryID, suffix string) (interface{}, bool) {
	switch cats := categoriesRaw.(type) {
	case map[string]map[string]float64:
		catMap, ok := cats[categoryID]
		if ok {
			if val, ok := catMap[suffix]; ok {
				return val, true
			}
		}
	case map[string]interface{}:
		if catRaw, ok := cats[categoryID]; ok {
			if catMap, ok := catRaw.(map[string]interface{}); ok {
				if val, ok := catMap[suffix]; ok {
					return val, true
				}
			}
		}
	}
	return nil, false
}

// evaluateCondition evaluates a single condition against the data.
// Supports both short form (eq, neq, gt, gte, lt, lte) and long form
// (equals, not_equals, greater_than, etc.) operator names.
func evaluateCondition(condition domain.BoostCondition, data map[string]interface{}) bool {
	fieldValue, exists := lookupFieldValue(condition, data)
	if !exists {
		return false
	}
	return evaluateConditionWithValue(condition, fieldValue)
}

// evaluateConditionWithValue evaluates a condition against a pre-looked-up value,
// avoiding a second map lookup when the caller already holds the value.
func evaluateConditionWithValue(condition domain.BoostCondition, fieldValue interface{}) bool {
	if fieldValue == nil {
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
	case json.Number:
		// json.Number arrives after a River JSON round-trip where numbers are
		// unmarshalled into interface{} (encoding/json uses json.Number when
		// UseNumber() is configured, or when map[string]interface{} re-reads
		// the EventData). Issue #1271 Item 6.
		f, err := v.Float64()
		if err != nil {
			return false
		}
		fieldNum = f
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

// markExecutionSkipped marks an execution as skipped with the given reason.
// Sets status, completion time, duration, and output data in one place.
func (s *boostExecutionService) markExecutionSkipped(execution *domain.BoostExecution, startTime time.Time, reason string) {
	execution.Status = ExecutionStatusSkipped
	completedAt := time.Now()
	execution.CompletedAt = &completedAt
	durationMs := int(time.Since(startTime).Milliseconds())
	execution.ExecutionDurationMs = &durationMs
	execution.OutputData, _ = json.Marshal(map[string]string{"skip_reason": reason})
}

// Helper functions for optional pointers.
func ptrTime(t time.Time) *time.Time {
	return &t
}

func ptrString(s string) *string {
	return &s
}
