package rules

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Test helper to create a transaction for testing
func newTestTransaction(merchantName string, amount float64, paymentChannel string, category []string) *domain.Transaction {
	return &domain.Transaction{
		MerchantName:   &merchantName,
		Amount:         amount,
		PaymentChannel: &paymentChannel,
		Category:       strPtr(mustMarshalJSON(category)),
	}
}

// ============================================================================
// Merchant Evaluator Tests
// ============================================================================

func TestMerchantContainsEvaluator_Evaluate(t *testing.T) {
	evaluator := &MerchantContainsEvaluator{}

	tests := []struct {
		name         string
		merchantName string
		condition    domain.RuleCondition
		want         bool
		wantErr      bool
	}{
		{
			name:         "matches substring (case insensitive)",
			merchantName: "STARBUCKS COFFEE #123",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			want:         true,
		},
		{
			name:         "matches exact case",
			merchantName: "Starbucks Coffee",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "Starbucks"},
			want:         true,
		},
		{
			name:         "no match",
			merchantName: "DUNKIN DONUTS",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			want:         false,
		},
		{
			name:         "empty merchant name",
			merchantName: "",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			want:         false,
		},
		{
			name:         "matches one of multiple values (array)",
			merchantName: "UBER TRIP",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: []interface{}{"uber", "lyft"}},
			want:         true,
		},
		{
			name:         "no match for any in array",
			merchantName: "TAXI SERVICE",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: []interface{}{"uber", "lyft"}},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(tt.merchantName, 10.0, "in store", nil)
			got, err := evaluator.Evaluate(tx, tt.condition)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerchantEqualsEvaluator_Evaluate(t *testing.T) {
	evaluator := &MerchantEqualsEvaluator{}

	tests := []struct {
		name         string
		merchantName string
		condition    domain.RuleCondition
		want         bool
	}{
		{
			name:         "exact match (case insensitive)",
			merchantName: "STARBUCKS",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantEquals, Value: "starbucks"},
			want:         true,
		},
		{
			name:         "partial match fails",
			merchantName: "STARBUCKS COFFEE",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantEquals, Value: "starbucks"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(tt.merchantName, 10.0, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerchantStartsWithEvaluator_Evaluate(t *testing.T) {
	evaluator := &MerchantStartsWithEvaluator{}

	tests := []struct {
		name         string
		merchantName string
		condition    domain.RuleCondition
		want         bool
	}{
		{
			name:         "starts with prefix",
			merchantName: "STARBUCKS COFFEE #123",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantStartsWith, Value: "starbucks"},
			want:         true,
		},
		{
			name:         "contains but doesn't start with",
			merchantName: "THE STARBUCKS RESERVE",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantStartsWith, Value: "starbucks"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(tt.merchantName, 10.0, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerchantEndsWithEvaluator_Evaluate(t *testing.T) {
	evaluator := &MerchantEndsWithEvaluator{}

	tests := []struct {
		name         string
		merchantName string
		condition    domain.RuleCondition
		want         bool
	}{
		{
			name:         "ends with suffix",
			merchantName: "THE COFFEE SHOP STARBUCKS",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantEndsWith, Value: "starbucks"},
			want:         true,
		},
		{
			name:         "contains but doesn't end with",
			merchantName: "STARBUCKS COFFEE #123",
			condition:    domain.RuleCondition{Type: domain.ConditionMerchantEndsWith, Value: "starbucks"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(tt.merchantName, 10.0, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Amount Evaluator Tests
// ============================================================================

func TestAmountGreaterThanEvaluator_Evaluate(t *testing.T) {
	evaluator := &AmountGreaterThanEvaluator{}

	tests := []struct {
		name      string
		amount    float64
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "amount greater than threshold",
			amount:    -600.00, // Negative = debit in BoostedBudget
			condition: domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: float64(500)},
			want:      true, // Uses absolute value
		},
		{
			name:      "amount equals threshold",
			amount:    -500.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: float64(500)},
			want:      false,
		},
		{
			name:      "amount less than threshold",
			amount:    -100.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: float64(500)},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", tt.amount, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountLessThanEvaluator_Evaluate(t *testing.T) {
	evaluator := &AmountLessThanEvaluator{}

	tests := []struct {
		name      string
		amount    float64
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "amount less than threshold",
			amount:    -15.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountLessThan, Value: float64(20)},
			want:      true,
		},
		{
			name:      "amount equals threshold",
			amount:    -20.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountLessThan, Value: float64(20)},
			want:      false,
		},
		{
			name:      "amount greater than threshold",
			amount:    -50.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountLessThan, Value: float64(20)},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", tt.amount, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountRangeEvaluator_Evaluate(t *testing.T) {
	evaluator := &AmountRangeEvaluator{}

	tests := []struct {
		name      string
		amount    float64
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:   "amount within range",
			amount: -50.00,
			condition: domain.RuleCondition{
				Type:  domain.ConditionAmountRange,
				Value: map[string]interface{}{"min": float64(10), "max": float64(100)},
			},
			want: true,
		},
		{
			name:   "amount equals min (inclusive)",
			amount: -10.00,
			condition: domain.RuleCondition{
				Type:  domain.ConditionAmountRange,
				Value: map[string]interface{}{"min": float64(10), "max": float64(100)},
			},
			want: true,
		},
		{
			name:   "amount equals max (inclusive)",
			amount: -100.00,
			condition: domain.RuleCondition{
				Type:  domain.ConditionAmountRange,
				Value: map[string]interface{}{"min": float64(10), "max": float64(100)},
			},
			want: true,
		},
		{
			name:   "amount below range",
			amount: -5.00,
			condition: domain.RuleCondition{
				Type:  domain.ConditionAmountRange,
				Value: map[string]interface{}{"min": float64(10), "max": float64(100)},
			},
			want: false,
		},
		{
			name:   "amount above range",
			amount: -150.00,
			condition: domain.RuleCondition{
				Type:  domain.ConditionAmountRange,
				Value: map[string]interface{}{"min": float64(10), "max": float64(100)},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", tt.amount, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Payment Channel Evaluator Tests
// ============================================================================

func TestPaymentChannelEvaluator_Evaluate(t *testing.T) {
	evaluator := &PaymentChannelEvaluator{}

	tests := []struct {
		name           string
		paymentChannel string
		condition      domain.RuleCondition
		want           bool
	}{
		{
			name:           "matches online",
			paymentChannel: "online",
			condition:      domain.RuleCondition{Type: domain.ConditionPaymentChannel, Value: "online"},
			want:           true,
		},
		{
			name:           "matches in store",
			paymentChannel: "in store",
			condition:      domain.RuleCondition{Type: domain.ConditionPaymentChannel, Value: "in store"},
			want:           true,
		},
		{
			name:           "no match",
			paymentChannel: "in store",
			condition:      domain.RuleCondition{Type: domain.ConditionPaymentChannel, Value: "online"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", 10.0, tt.paymentChannel, nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Registry Tests
// ============================================================================

func TestConditionRegistry_Evaluate(t *testing.T) {
	registry := NewConditionRegistry()

	tests := []struct {
		name      string
		tx        *domain.Transaction
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "merchant contains match",
			tx:        newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			condition: domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			want:      true,
		},
		{
			name:      "amount greater than match",
			tx:        newTestTransaction("BEST BUY", -600.00, "in store", nil),
			condition: domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: float64(500)},
			want:      true,
		},
		{
			name:      "payment channel match",
			tx:        newTestTransaction("AMAZON", -50.00, "online", nil),
			condition: domain.RuleCondition{Type: domain.ConditionPaymentChannel, Value: "online"},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registry.Evaluate(tt.tx, tt.condition)
			if err != nil {
				t.Errorf("Evaluate() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionRegistry_UnknownType_ReturnsFalse(t *testing.T) {
	registry := NewConditionRegistry()
	tx := newTestTransaction("MERCHANT", 10.0, "in store", nil)
	condition := domain.RuleCondition{Type: domain.ConditionType("unknown_type"), Value: "test"}

	got, err := registry.Evaluate(tx, condition)

	// Should return false with no error (graceful fallback)
	if err != nil {
		t.Errorf("Evaluate() error = %v, expected nil for unknown type", err)
	}
	if got != false {
		t.Errorf("Evaluate() = %v, want false for unknown type", got)
	}
}

// ============================================================================
// Additional Evaluator Tests (REFACTOR phase)
// ============================================================================

func TestAccountIDEvaluator_Evaluate(t *testing.T) {
	evaluator := &AccountIDEvaluator{}

	tests := []struct {
		name      string
		accountID string
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "matches single account ID",
			accountID: "acc-123",
			condition: domain.RuleCondition{Type: domain.ConditionAccountID, Value: "acc-123"},
			want:      true,
		},
		{
			name:      "matches one of multiple account IDs",
			accountID: "acc-456",
			condition: domain.RuleCondition{Type: domain.ConditionAccountID, Value: []interface{}{"acc-123", "acc-456", "acc-789"}},
			want:      true,
		},
		{
			name:      "no match",
			accountID: "acc-999",
			condition: domain.RuleCondition{Type: domain.ConditionAccountID, Value: "acc-123"},
			want:      false,
		},
		{
			name:      "empty account ID",
			accountID: "",
			condition: domain.RuleCondition{Type: domain.ConditionAccountID, Value: "acc-123"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", 10.0, "in store", nil)
			tx.AccountID = tt.accountID
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExistingCategoryEvaluator_Evaluate(t *testing.T) {
	evaluator := &ExistingCategoryEvaluator{}

	tests := []struct {
		name      string
		category  []string
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "matches primary category",
			category:  []string{"Food and Drink", "Restaurants"},
			condition: domain.RuleCondition{Type: domain.ConditionExistingCategory, Value: "Food and Drink"},
			want:      true,
		},
		{
			name:      "matches subcategory",
			category:  []string{"Food and Drink", "Restaurants"},
			condition: domain.RuleCondition{Type: domain.ConditionExistingCategory, Value: "restaurants"},
			want:      true,
		},
		{
			name:      "matches one of multiple values",
			category:  []string{"Travel", "Airlines"},
			condition: domain.RuleCondition{Type: domain.ConditionExistingCategory, Value: []interface{}{"Travel", "Transportation"}},
			want:      true,
		},
		{
			name:      "no match",
			category:  []string{"Food and Drink", "Restaurants"},
			condition: domain.RuleCondition{Type: domain.ConditionExistingCategory, Value: "Travel"},
			want:      false,
		},
		{
			name:      "nil category",
			category:  nil,
			condition: domain.RuleCondition{Type: domain.ConditionExistingCategory, Value: "Food"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", 10.0, "in store", tt.category)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateRangeEvaluator_Evaluate(t *testing.T) {
	evaluator := &DateRangeEvaluator{}

	tests := []struct {
		name      string
		date      string // YYYY-MM-DD
		condition domain.RuleCondition
		want      bool
		wantErr   bool
	}{
		{
			name: "within range",
			date: "2024-06-15",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01", "end": "2024-06-30"},
			},
			want: true,
		},
		{
			name: "equals start date (inclusive)",
			date: "2024-06-01",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01", "end": "2024-06-30"},
			},
			want: true,
		},
		{
			name: "equals end date (inclusive)",
			date: "2024-06-30",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01", "end": "2024-06-30"},
			},
			want: true,
		},
		{
			name: "before range",
			date: "2024-05-15",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01", "end": "2024-06-30"},
			},
			want: false,
		},
		{
			name: "after range",
			date: "2024-07-15",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01", "end": "2024-06-30"},
			},
			want: false,
		},
		{
			name: "only start bound",
			date: "2024-12-01",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"start": "2024-06-01"},
			},
			want: true,
		},
		{
			name: "only end bound",
			date: "2024-03-01",
			condition: domain.RuleCondition{
				Type:  domain.ConditionDateRange,
				Value: map[string]interface{}{"end": "2024-06-30"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", 10.0, "in store", nil)
			tx.Date = mustParseDate(tt.date)
			got, err := evaluator.Evaluate(tx, tt.condition)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountEqualsEvaluator_Evaluate(t *testing.T) {
	evaluator := &AmountEqualsEvaluator{}

	tests := []struct {
		name      string
		amount    float64
		condition domain.RuleCondition
		want      bool
	}{
		{
			name:      "exact match (negative amount)",
			amount:    -50.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountEquals, Value: float64(50)},
			want:      true,
		},
		{
			name:      "exact match (positive amount)",
			amount:    50.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountEquals, Value: float64(50)},
			want:      true,
		},
		{
			name:      "no match",
			amount:    -50.00,
			condition: domain.RuleCondition{Type: domain.ConditionAmountEquals, Value: float64(100)},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction("MERCHANT", tt.amount, "in store", nil)
			got, _ := evaluator.Evaluate(tx, tt.condition)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}
