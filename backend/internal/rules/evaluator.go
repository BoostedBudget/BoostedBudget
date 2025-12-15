package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/matching"
)

// ConditionEvaluator evaluates a single condition against a transaction.
type ConditionEvaluator interface {
	// Evaluate returns true if the transaction matches the condition.
	Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error)
}

// ConditionRegistry manages condition evaluators by type.
type ConditionRegistry struct {
	evaluators map[domain.ConditionType]ConditionEvaluator
}

// NewConditionRegistry creates a registry with all built-in evaluators.
func NewConditionRegistry() *ConditionRegistry {
	r := &ConditionRegistry{
		evaluators: make(map[domain.ConditionType]ConditionEvaluator),
	}

	// Register merchant evaluators
	r.evaluators[domain.ConditionMerchantContains] = &MerchantContainsEvaluator{}
	r.evaluators[domain.ConditionMerchantEquals] = &MerchantEqualsEvaluator{}
	r.evaluators[domain.ConditionMerchantStartsWith] = &MerchantStartsWithEvaluator{}
	r.evaluators[domain.ConditionMerchantEndsWith] = &MerchantEndsWithEvaluator{}

	// Register amount evaluators
	r.evaluators[domain.ConditionAmountGreaterThan] = &AmountGreaterThanEvaluator{}
	r.evaluators[domain.ConditionAmountLessThan] = &AmountLessThanEvaluator{}
	r.evaluators[domain.ConditionAmountEquals] = &AmountEqualsEvaluator{}
	r.evaluators[domain.ConditionAmountRange] = &AmountRangeEvaluator{}

	// Register other evaluators
	r.evaluators[domain.ConditionPaymentChannel] = &PaymentChannelEvaluator{}
	r.evaluators[domain.ConditionAccountID] = &AccountIDEvaluator{}
	r.evaluators[domain.ConditionAccountType] = &AccountTypeEvaluator{}
	r.evaluators[domain.ConditionExistingCategory] = &ExistingCategoryEvaluator{}
	r.evaluators[domain.ConditionDateRange] = &DateRangeEvaluator{}

	return r
}

// Evaluate evaluates a condition using the appropriate evaluator.
// Returns false (no error) for unknown condition types (graceful fallback).
func (r *ConditionRegistry) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	evaluator, ok := r.evaluators[condition.Type]
	if !ok {
		// Unknown condition type - gracefully return false (no match)
		return false, nil
	}
	return evaluator.Evaluate(tx, condition)
}

// Register adds or replaces an evaluator for a condition type.
func (r *ConditionRegistry) Register(conditionType domain.ConditionType, evaluator ConditionEvaluator) {
	r.evaluators[conditionType] = evaluator
}

// ============================================================================
// Merchant Evaluators
// ============================================================================

// MerchantContainsEvaluator checks if merchant name contains a substring.
type MerchantContainsEvaluator struct{}

func (e *MerchantContainsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	merchantName := getMerchantName(tx)
	if merchantName == "" {
		return false, nil
	}

	// Handle both single string and array of strings
	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	merchantLower := strings.ToLower(merchantName)
	for _, v := range values {
		if strings.Contains(merchantLower, strings.ToLower(v)) {
			return true, nil
		}
	}
	return false, nil
}

// MerchantEqualsEvaluator checks if merchant name exactly matches.
type MerchantEqualsEvaluator struct{}

func (e *MerchantEqualsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	merchantName := getMerchantName(tx)
	if merchantName == "" {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	merchantLower := strings.ToLower(merchantName)
	for _, v := range values {
		if merchantLower == strings.ToLower(v) {
			return true, nil
		}
	}
	return false, nil
}

// MerchantStartsWithEvaluator checks if merchant name starts with prefix.
type MerchantStartsWithEvaluator struct{}

func (e *MerchantStartsWithEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	merchantName := getMerchantName(tx)
	if merchantName == "" {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	merchantLower := strings.ToLower(merchantName)
	for _, v := range values {
		if strings.HasPrefix(merchantLower, strings.ToLower(v)) {
			return true, nil
		}
	}
	return false, nil
}

// MerchantEndsWithEvaluator checks if merchant name ends with suffix.
type MerchantEndsWithEvaluator struct{}

func (e *MerchantEndsWithEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	merchantName := getMerchantName(tx)
	if merchantName == "" {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	merchantLower := strings.ToLower(merchantName)
	for _, v := range values {
		if strings.HasSuffix(merchantLower, strings.ToLower(v)) {
			return true, nil
		}
	}
	return false, nil
}

// ============================================================================
// Amount Evaluators
// ============================================================================

// AmountGreaterThanEvaluator checks if absolute amount exceeds threshold.
type AmountGreaterThanEvaluator struct{}

func (e *AmountGreaterThanEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	threshold, ok := extractFloat(condition.Value)
	if !ok {
		return false, fmt.Errorf("invalid amount threshold: %v", condition.Value)
	}

	// Use absolute value (transactions can be negative for debits)
	absAmount := math.Abs(tx.Amount)
	return absAmount > threshold, nil
}

// AmountLessThanEvaluator checks if absolute amount is below threshold.
type AmountLessThanEvaluator struct{}

func (e *AmountLessThanEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	threshold, ok := extractFloat(condition.Value)
	if !ok {
		return false, fmt.Errorf("invalid amount threshold: %v", condition.Value)
	}

	absAmount := math.Abs(tx.Amount)
	return absAmount < threshold, nil
}

// AmountEqualsEvaluator checks if absolute amount equals value (with small epsilon).
type AmountEqualsEvaluator struct{}

func (e *AmountEqualsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	target, ok := extractFloat(condition.Value)
	if !ok {
		return false, fmt.Errorf("invalid amount value: %v", condition.Value)
	}

	absAmount := math.Abs(tx.Amount)
	// Use small epsilon for float comparison
	const epsilon = 0.001
	return math.Abs(absAmount-target) < epsilon, nil
}

// AmountRangeEvaluator checks if absolute amount is within range (inclusive).
type AmountRangeEvaluator struct{}

func (e *AmountRangeEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	rangeMap, ok := condition.Value.(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid amount range: expected map with min/max")
	}

	minVal, minOk := extractFloat(rangeMap["min"])
	maxVal, maxOk := extractFloat(rangeMap["max"])
	if !minOk || !maxOk {
		return false, fmt.Errorf("invalid amount range: min and max must be numbers")
	}

	absAmount := math.Abs(tx.Amount)
	return absAmount >= minVal && absAmount <= maxVal, nil
}

// ============================================================================
// Other Evaluators
// ============================================================================

// PaymentChannelEvaluator checks payment channel (online, in store, etc).
type PaymentChannelEvaluator struct{}

func (e *PaymentChannelEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	if tx.PaymentChannel == nil {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	channelLower := strings.ToLower(*tx.PaymentChannel)
	for _, v := range values {
		if channelLower == strings.ToLower(v) {
			return true, nil
		}
	}
	return false, nil
}

// AccountIDEvaluator checks if transaction belongs to specific account(s).
type AccountIDEvaluator struct{}

// AccountTypeEvaluator checks if transaction's account is of a specific type.
// Uses OR logic: matches if account type is ANY of the specified types.
type AccountTypeEvaluator struct{}

func (e *AccountIDEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	if tx.AccountID == "" {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	for _, v := range values {
		if tx.AccountID == v {
			return true, nil
		}
	}
	return false, nil
}

func (e *AccountTypeEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	// Transaction must have an associated Account with a Type
	if tx.Account == nil || tx.Account.Type == "" {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	// OR logic: match if account type is ANY of the specified types
	txAccountType := strings.ToLower(tx.Account.Type)
	for _, v := range values {
		if strings.EqualFold(txAccountType, v) {
			return true, nil
		}
	}
	return false, nil
}

// ExistingCategoryEvaluator checks transaction's existing (Plaid) category.
// Only matches against the primary category (index 0) for consistency.
type ExistingCategoryEvaluator struct{}

func (e *ExistingCategoryEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	if tx.Category == nil {
		return false, nil
	}

	// Parse the category JSON
	var categories []string
	if err := json.Unmarshal([]byte(*tx.Category), &categories); err != nil {
		return false, nil
	}

	// Must have at least one category (primary)
	if len(categories) == 0 {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	// Only check primary category (index 0) for consistency
	primaryCategory := strings.ToLower(categories[0])
	for _, v := range values {
		if primaryCategory == strings.ToLower(v) {
			return true, nil
		}
	}
	return false, nil
}

// DateRangeEvaluator checks if transaction date falls within range.
type DateRangeEvaluator struct{}

func (e *DateRangeEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	rangeMap, ok := condition.Value.(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid date range: expected map with start/end")
	}

	txDate := tx.Date

	startStr, startOk := rangeMap["start"].(string)
	endStr, endOk := rangeMap["end"].(string)

	// At least one bound must be specified
	if !startOk && !endOk {
		return false, fmt.Errorf("date range must have at least start or end")
	}

	// Parse date strings (YYYY-MM-DD format)
	const dateLayout = "2006-01-02"

	// Check start bound (inclusive)
	if startOk {
		startDate, err := time.Parse(dateLayout, startStr)
		if err != nil {
			return false, fmt.Errorf("invalid start date format: %s", startStr)
		}
		if txDate.Before(startDate) {
			return false, nil
		}
	}

	// Check end bound (inclusive - add 1 day to include the end date fully)
	if endOk {
		endDate, err := time.Parse(dateLayout, endStr)
		if err != nil {
			return false, fmt.Errorf("invalid end date format: %s", endStr)
		}
		// Add 1 day to make end date inclusive
		endDate = endDate.AddDate(0, 0, 1)
		if txDate.After(endDate) || txDate.Equal(endDate) {
			return false, nil
		}
	}

	return true, nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// getMerchantName safely extracts merchant name from transaction.
func getMerchantName(tx *domain.Transaction) string {
	if tx.MerchantName != nil {
		return *tx.MerchantName
	}
	return ""
}

// Note: extractStringValues moved to internal/matching package (Issue #495)

// extractFloat extracts a float64 from interface{}.
func extractFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
