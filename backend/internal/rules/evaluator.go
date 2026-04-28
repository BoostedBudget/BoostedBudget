package rules

import (
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
	r.evaluators[domain.ConditionDateRange] = &DateRangeEvaluator{}
	r.evaluators[domain.ConditionExistingPFCPrimary] = &ExistingPFCPrimaryEvaluator{}
	r.evaluators[domain.ConditionExistingPFCDetailed] = &ExistingPFCDetailedEvaluator{}

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

// merchantMatchFunc defines a function that checks if a merchant matches a value.
type merchantMatchFunc func(merchant, value string) bool

// evaluateMerchantCondition is a helper that evaluates merchant conditions with custom match logic.
func evaluateMerchantCondition(tx *domain.Transaction, condition domain.RuleCondition, matchFn merchantMatchFunc) (bool, error) {
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
		if matchFn(merchantLower, strings.ToLower(v)) {
			return true, nil
		}
	}
	return false, nil
}

// MerchantContainsEvaluator checks if merchant name contains a substring.
type MerchantContainsEvaluator struct{}

func (e *MerchantContainsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateMerchantCondition(tx, condition, strings.Contains)
}

// MerchantEqualsEvaluator checks if merchant name exactly matches.
type MerchantEqualsEvaluator struct{}

func (e *MerchantEqualsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateMerchantCondition(tx, condition, func(merchant, value string) bool {
		return merchant == value
	})
}

// MerchantStartsWithEvaluator checks if merchant name starts with prefix.
type MerchantStartsWithEvaluator struct{}

func (e *MerchantStartsWithEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateMerchantCondition(tx, condition, strings.HasPrefix)
}

// MerchantEndsWithEvaluator checks if merchant name ends with suffix.
type MerchantEndsWithEvaluator struct{}

func (e *MerchantEndsWithEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateMerchantCondition(tx, condition, strings.HasSuffix)
}

// ============================================================================
// Amount Evaluators
// ============================================================================

// amountCompareFn defines a function that compares an absolute amount against a threshold.
type amountCompareFn func(absAmount, threshold float64) bool

// evaluateAmountCondition is a helper that evaluates amount conditions.
func evaluateAmountCondition(tx *domain.Transaction, condition domain.RuleCondition, compareFn amountCompareFn) (bool, error) {
	threshold, ok := extractFloat(condition.Value)
	if !ok {
		return false, fmt.Errorf("invalid amount: %v", condition.Value)
	}
	return compareFn(math.Abs(tx.Amount), threshold), nil
}

// AmountGreaterThanEvaluator checks if absolute amount exceeds threshold.
type AmountGreaterThanEvaluator struct{}

func (e *AmountGreaterThanEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateAmountCondition(tx, condition, func(absAmount, threshold float64) bool {
		return absAmount > threshold
	})
}

// AmountLessThanEvaluator checks if absolute amount is below threshold.
type AmountLessThanEvaluator struct{}

func (e *AmountLessThanEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	return evaluateAmountCondition(tx, condition, func(absAmount, threshold float64) bool {
		return absAmount < threshold
	})
}

// AmountEqualsEvaluator checks if absolute amount equals value (with small epsilon).
type AmountEqualsEvaluator struct{}

func (e *AmountEqualsEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	const epsilon = 0.001
	return evaluateAmountCondition(tx, condition, func(absAmount, threshold float64) bool {
		return math.Abs(absAmount-threshold) < epsilon
	})
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

// ExistingPFCPrimaryEvaluator checks against the Plaid PFC primary field (e.g. "FOOD_AND_DRINK").
// Uses the effective category: override if set, else source pfc_primary.
type ExistingPFCPrimaryEvaluator struct{}

func (e *ExistingPFCPrimaryEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	effective := tx.GetEffectiveCategory()
	if effective == nil || effective.Kind != domain.EffectiveCategoryKindPFC || effective.PFCPrimary == nil {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	primary := strings.ToLower(*effective.PFCPrimary)
	for _, v := range values {
		if primary == strings.ToLower(v) {
			return true, nil
		}
	}
	return false, nil
}

// ExistingPFCDetailedEvaluator checks against the Plaid PFC detailed field (e.g. "FOOD_AND_DRINK_FAST_FOOD").
// Uses the effective category: override if set, else source pfc_detailed.
type ExistingPFCDetailedEvaluator struct{}

func (e *ExistingPFCDetailedEvaluator) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	effective := tx.GetEffectiveCategory()
	if effective == nil || effective.Kind != domain.EffectiveCategoryKindPFC || effective.PFCDetailed == nil {
		return false, nil
	}

	values := matching.ExtractStringValues(condition.Value)
	if len(values) == 0 {
		return false, nil
	}

	detailed := strings.ToLower(*effective.PFCDetailed)
	for _, v := range values {
		if detailed == strings.ToLower(v) {
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
