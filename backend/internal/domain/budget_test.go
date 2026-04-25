package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestBudget_Validate tests that Budget validates required fields
func TestBudget_Validate(t *testing.T) {
	validStartDate := time.Now()
	validEndDate := validStartDate.AddDate(0, 1, 0) // 1 month later

	tests := []struct {
		name    string
		budget  Budget
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid monthly budget",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Groceries",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "valid custom period budget with end date",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Vacation Budget",
				Period:         BudgetPeriodCustom,
				StartDate:      validStartDate,
				EndDate:        &validEndDate,
				Amount:         2000.00,
				ExpectedIncome: 2000.00,
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			budget: Budget{
				Name:         "Test Budget",
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing name",
			budget: Budget{
				UserID:       "user-123",
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "name too long",
			budget: Budget{
				UserID:       "user-123",
				Name:         string(make([]byte, 101)), // 101 characters
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "name must be 100 characters or less",
		},
		{
			name: "invalid period",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Test Budget",
				Period:       BudgetPeriod("invalid"),
				StartDate:    validStartDate,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "invalid budget period",
		},
		{
			name: "missing start_date",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Test Budget",
				Period:       BudgetPeriodMonthly,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "start_date is required",
		},
		{
			name: "zero amount",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Test Budget",
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       0,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "amount must be positive",
		},
		{
			name: "negative amount",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Test Budget",
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       -100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "amount must be positive",
		},
		{
			name: "missing currency_code",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Test Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         100.00,
				ExpectedIncome: 100.00,
			},
			wantErr: true,
			errMsg:  "currency_code is required",
		},
		{
			name: "custom period without end_date",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Custom Budget",
				Period:         BudgetPeriodCustom,
				StartDate:      validStartDate,
				Amount:         100.00,
				ExpectedIncome: 100.00,
				CurrencyCode:   "USD",
			},
			wantErr: true,
			errMsg:  "end_date is required for custom period budgets",
		},
		{
			name: "end_date before start_date",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Test Budget",
				Period:         BudgetPeriodCustom,
				StartDate:      validEndDate,   // later date as start
				EndDate:        &validStartDate, // earlier date as end
				Amount:         100.00,
				ExpectedIncome: 100.00,
				CurrencyCode:   "USD",
			},
			wantErr: true,
			errMsg:  "end_date must be after start_date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.budget.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestBudget_Validate_RequiresExpectedIncomePositive tests Issue #1048 domain invariant:
// expected_income must be positive after service reconciliation sets it.
func TestBudget_Validate_RequiresExpectedIncomePositive(t *testing.T) {
	validStartDate := time.Now()

	base := Budget{
		UserID:       "user-123",
		Name:         "Test Budget",
		Period:       BudgetPeriodMonthly,
		StartDate:    validStartDate,
		Amount:       100.00,
		CurrencyCode: "USD",
	}

	t.Run("zero expected_income is rejected", func(t *testing.T) {
		b := base
		b.ExpectedIncome = 0
		err := b.Validate()
		if err == nil {
			t.Fatal("Validate() expected error for ExpectedIncome=0, got nil")
		}
		if !contains(err.Error(), "expected_income") {
			t.Errorf("error should mention expected_income, got: %v", err)
		}
	})

	t.Run("negative expected_income is rejected", func(t *testing.T) {
		b := base
		b.ExpectedIncome = -1.0
		err := b.Validate()
		if err == nil {
			t.Fatal("Validate() expected error for ExpectedIncome=-1, got nil")
		}
		if !contains(err.Error(), "expected_income") {
			t.Errorf("error should mention expected_income, got: %v", err)
		}
	})

	t.Run("positive expected_income is accepted", func(t *testing.T) {
		b := base
		b.ExpectedIncome = 100.00
		err := b.Validate()
		if err != nil {
			t.Errorf("Validate() unexpected error for ExpectedIncome=100: %v", err)
		}
	})
}

// TestBudgetPeriod_IsValid tests the IsValid helper for budget periods
func TestBudgetPeriod_IsValid(t *testing.T) {
	validPeriods := []BudgetPeriod{
		BudgetPeriodWeekly,
		BudgetPeriodMonthly,
		BudgetPeriodQuarterly,
		BudgetPeriodYearly,
		BudgetPeriodCustom,
	}

	for _, bp := range validPeriods {
		t.Run(string(bp), func(t *testing.T) {
			if !bp.IsValid() {
				t.Errorf("IsValid() = false, want true for %s", bp)
			}
		})
	}

	// Test invalid period
	invalidPeriod := BudgetPeriod("invalid")
	if invalidPeriod.IsValid() {
		t.Errorf("IsValid() = true, want false for invalid period")
	}
}

// TestBudget_TotalSpent tests the TotalSpent calculation
func TestBudget_TotalSpent(t *testing.T) {
	tests := []struct {
		name       string
		categories []BudgetCategory
		want       float64
	}{
		{
			name:       "empty categories",
			categories: []BudgetCategory{},
			want:       0,
		},
		{
			name: "single category",
			categories: []BudgetCategory{
				{SpentAmount: 150.50},
			},
			want: 150.50,
		},
		{
			name: "multiple categories",
			categories: []BudgetCategory{
				{SpentAmount: 100.00},
				{SpentAmount: 250.50},
				{SpentAmount: 75.25},
			},
			want: 425.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{Categories: tt.categories}
			if got := budget.TotalSpent(); got != tt.want {
				t.Errorf("TotalSpent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_TotalAllocated tests the TotalAllocated calculation
func TestBudget_TotalAllocated(t *testing.T) {
	tests := []struct {
		name       string
		categories []BudgetCategory
		want       float64
	}{
		{
			name:       "empty categories",
			categories: []BudgetCategory{},
			want:       0,
		},
		{
			name: "single category",
			categories: []BudgetCategory{
				{AllocatedAmount: 200.00},
			},
			want: 200.00,
		},
		{
			name: "multiple categories",
			categories: []BudgetCategory{
				{AllocatedAmount: 100.00},
				{AllocatedAmount: 300.00},
				{AllocatedAmount: 100.00},
			},
			want: 500.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{Categories: tt.categories}
			if got := budget.TotalAllocated(); got != tt.want {
				t.Errorf("TotalAllocated() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_RemainingAmount tests the RemainingAmount calculation
func TestBudget_RemainingAmount(t *testing.T) {
	tests := []struct {
		name       string
		amount     float64
		categories []BudgetCategory
		want       float64
	}{
		{
			name:       "full budget remaining",
			amount:     500.00,
			categories: []BudgetCategory{},
			want:       500.00,
		},
		{
			name:   "partial spending",
			amount: 500.00,
			categories: []BudgetCategory{
				{SpentAmount: 150.00},
				{SpentAmount: 100.00},
			},
			want: 250.00,
		},
		{
			name:   "over budget (negative remaining)",
			amount: 300.00,
			categories: []BudgetCategory{
				{SpentAmount: 200.00},
				{SpentAmount: 200.00},
			},
			want: -100.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				Amount:     tt.amount,
				Categories: tt.categories,
			}
			if got := budget.RemainingAmount(); got != tt.want {
				t.Errorf("RemainingAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_IsOverBudget tests the IsOverBudget check
func TestBudget_IsOverBudget(t *testing.T) {
	tests := []struct {
		name       string
		amount     float64
		categories []BudgetCategory
		want       bool
	}{
		{
			name:       "under budget",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 300.00}},
			want:       false,
		},
		{
			name:       "exactly at budget",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 500.00}},
			want:       false,
		},
		{
			name:       "over budget",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 600.00}},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				Amount:     tt.amount,
				Categories: tt.categories,
			}
			if got := budget.IsOverBudget(); got != tt.want {
				t.Errorf("IsOverBudget() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_PercentUsed tests the PercentUsed calculation
func TestBudget_PercentUsed(t *testing.T) {
	tests := []struct {
		name       string
		amount     float64
		categories []BudgetCategory
		want       float64
	}{
		{
			name:       "zero budget zero spent",
			amount:     0,
			categories: []BudgetCategory{},
			want:       0,
		},
		{
			name:       "zero budget with spending",
			amount:     0,
			categories: []BudgetCategory{{SpentAmount: 100.00}},
			want:       100,
		},
		{
			name:       "50% used",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 250.00}},
			want:       50,
		},
		{
			name:       "100% used",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 500.00}},
			want:       100,
		},
		{
			name:       "over 100% (over budget)",
			amount:     500.00,
			categories: []BudgetCategory{{SpentAmount: 750.00}},
			want:       150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				Amount:     tt.amount,
				Categories: tt.categories,
			}
			if got := budget.PercentUsed(); got != tt.want {
				t.Errorf("PercentUsed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudgetCategory_Validate tests that BudgetCategory validates required fields
func TestBudgetCategory_Validate(t *testing.T) {
	tests := []struct {
		name     string
		category BudgetCategory
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid category",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink", "Coffee Shops"},
				AllocatedAmount: 100.00,
				SpentAmount:     50.00,
			},
			wantErr: false,
		},
		{
			name: "missing budget_id",
			category: BudgetCategory{
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
			},
			wantErr: true,
			errMsg:  "budget_id is required",
		},
		{
			name: "missing category",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				AllocatedAmount: 100.00,
			},
			wantErr: true,
			errMsg:  "category is required",
		},
		{
			name: "empty category array",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{},
				AllocatedAmount: 100.00,
			},
			wantErr: true,
			errMsg:  "category is required",
		},
		{
			name: "negative allocated_amount",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: -50.00,
			},
			wantErr: true,
			errMsg:  "allocated_amount cannot be negative",
		},
		{
			name: "negative spent_amount",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				SpentAmount:     -25.00,
			},
			wantErr: true,
			errMsg:  "spent_amount cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.category.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestBudgetCategory_RemainingAmount tests the RemainingAmount calculation
func TestBudgetCategory_RemainingAmount(t *testing.T) {
	tests := []struct {
		name            string
		allocatedAmount float64
		spentAmount     float64
		want            float64
	}{
		{
			name:            "full amount remaining",
			allocatedAmount: 200.00,
			spentAmount:     0,
			want:            200.00,
		},
		{
			name:            "partial spending",
			allocatedAmount: 200.00,
			spentAmount:     75.00,
			want:            125.00,
		},
		{
			name:            "over budget (negative remaining)",
			allocatedAmount: 100.00,
			spentAmount:     150.00,
			want:            -50.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := BudgetCategory{
				AllocatedAmount: tt.allocatedAmount,
				SpentAmount:     tt.spentAmount,
			}
			if got := bc.RemainingAmount(); got != tt.want {
				t.Errorf("RemainingAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudgetCategory_IsOverBudget tests the IsOverBudget check
func TestBudgetCategory_IsOverBudget(t *testing.T) {
	tests := []struct {
		name            string
		allocatedAmount float64
		spentAmount     float64
		want            bool
	}{
		{
			name:            "under budget",
			allocatedAmount: 200.00,
			spentAmount:     100.00,
			want:            false,
		},
		{
			name:            "exactly at budget",
			allocatedAmount: 200.00,
			spentAmount:     200.00,
			want:            false,
		},
		{
			name:            "over budget",
			allocatedAmount: 200.00,
			spentAmount:     250.00,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := BudgetCategory{
				AllocatedAmount: tt.allocatedAmount,
				SpentAmount:     tt.spentAmount,
			}
			if got := bc.IsOverBudget(); got != tt.want {
				t.Errorf("IsOverBudget() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudgetCategory_PercentUsed tests the PercentUsed calculation
func TestBudgetCategory_PercentUsed(t *testing.T) {
	tests := []struct {
		name            string
		allocatedAmount float64
		spentAmount     float64
		want            float64
	}{
		{
			name:            "zero allocation zero spent",
			allocatedAmount: 0,
			spentAmount:     0,
			want:            0,
		},
		{
			name:            "zero allocation with spending",
			allocatedAmount: 0,
			spentAmount:     50.00,
			want:            100,
		},
		{
			name:            "50% used",
			allocatedAmount: 200.00,
			spentAmount:     100.00,
			want:            50,
		},
		{
			name:            "100% used",
			allocatedAmount: 200.00,
			spentAmount:     200.00,
			want:            100,
		},
		{
			name:            "over 100%",
			allocatedAmount: 200.00,
			spentAmount:     300.00,
			want:            150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := BudgetCategory{
				AllocatedAmount: tt.allocatedAmount,
				SpentAmount:     tt.spentAmount,
			}
			if got := bc.PercentUsed(); got != tt.want {
				t.Errorf("PercentUsed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_JSONMarshaling tests JSON serialization
func TestBudget_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	endDate := now.AddDate(0, 1, 0)
	description := "Monthly food budget"

	original := &Budget{
		ID:           "budget-550e8400-e29b-41d4-a716-446655440000",
		UserID:       "user-650e8400-e29b-41d4-a716-446655440001",
		Name:         "Monthly Food",
		Description:  &description,
		Period:       BudgetPeriodMonthly,
		StartDate:    now,
		EndDate:      &endDate,
		Amount:       500.00,
		CurrencyCode: "USD",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
		Categories: []BudgetCategory{
			{
				ID:              "cat-1",
				BudgetID:        "budget-550e8400-e29b-41d4-a716-446655440000",
				Category:        []string{"Food & Drink", "Groceries"},
				AllocatedAmount: 300.00,
				SpentAmount:     150.00,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Budget: %v", err)
	}

	// Unmarshal back
	var unmarshaled Budget
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal Budget: %v", err)
	}

	// Verify critical fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.Name != original.Name {
		t.Errorf("Name mismatch: got %v, want %v", unmarshaled.Name, original.Name)
	}
	if unmarshaled.Period != original.Period {
		t.Errorf("Period mismatch: got %v, want %v", unmarshaled.Period, original.Period)
	}
	if unmarshaled.Amount != original.Amount {
		t.Errorf("Amount mismatch: got %v, want %v", unmarshaled.Amount, original.Amount)
	}
	if unmarshaled.CurrencyCode != original.CurrencyCode {
		t.Errorf("CurrencyCode mismatch: got %v, want %v", unmarshaled.CurrencyCode, original.CurrencyCode)
	}
	if len(unmarshaled.Categories) != len(original.Categories) {
		t.Errorf("Categories length mismatch: got %v, want %v", len(unmarshaled.Categories), len(original.Categories))
	}
}

// =============================================================================
// Issue #491: Envelope Budgeting Enhancement Tests
// =============================================================================

// TestIncomeSourceType_IsValid tests the IsValid helper for income source types
func TestIncomeSourceType_IsValid(t *testing.T) {
	tests := []struct {
		name       string
		sourceType IncomeSourceType
		want       bool
	}{
		{
			name:       "plaid_category valid",
			sourceType: IncomeSourceTypePlaidCategory,
			want:       true,
		},
		{
			name:       "tag valid",
			sourceType: IncomeSourceTypeTag,
			want:       true,
		},
		{
			name:       "manual valid",
			sourceType: IncomeSourceTypeManual,
			want:       true,
		},
		{
			name:       "empty invalid",
			sourceType: IncomeSourceType(""),
			want:       false,
		},
		{
			name:       "unknown invalid",
			sourceType: IncomeSourceType("unknown"),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sourceType.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIncomeSource_Validate tests IncomeSource validation
func TestIncomeSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		source  IncomeSource
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid plaid_category source",
			source: IncomeSource{
				Type:  IncomeSourceTypePlaidCategory,
				Value: "Payroll",
			},
			wantErr: false,
		},
		{
			name: "valid tag source",
			source: IncomeSource{
				Type:  IncomeSourceTypeTag,
				Value: "income",
			},
			wantErr: false,
		},
		{
			name: "valid manual source with description",
			source: IncomeSource{
				Type:        IncomeSourceTypeManual,
				Value:       "Side gig",
				Description: "Freelance work",
			},
			wantErr: false,
		},
		{
			name: "invalid type",
			source: IncomeSource{
				Type:  IncomeSourceType("invalid"),
				Value: "test",
			},
			wantErr: true,
			errMsg:  "invalid income source type",
		},
		{
			name: "missing value",
			source: IncomeSource{
				Type:  IncomeSourceTypeManual,
				Value: "",
			},
			wantErr: true,
			errMsg:  "income source value is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestMatchLogic_IsValid tests the IsValid helper for match logic values
func TestMatchLogic_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		logic MatchLogic
		want  bool
	}{
		{
			name:  "category_only valid",
			logic: MatchLogicCategoryOnly,
			want:  true,
		},
		{
			name:  "tag_only valid",
			logic: MatchLogicTagOnly,
			want:  true,
		},
		{
			name:  "and valid",
			logic: MatchLogicAnd,
			want:  true,
		},
		{
			name:  "or valid",
			logic: MatchLogicOr,
			want:  true,
		},
		{
			name:  "empty invalid",
			logic: MatchLogic(""),
			want:  false,
		},
		{
			name:  "unknown invalid",
			logic: MatchLogic("unknown"),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.logic.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_UnallocatedAmount tests the UnallocatedAmount calculation for envelope budgeting.
// UnallocatedAmount must use ExpectedIncome, not IncomeAmount (Issue #1048).
func TestBudget_UnallocatedAmount(t *testing.T) {
	tests := []struct {
		name           string
		expectedIncome float64
		categories     []BudgetCategory
		want           float64
	}{
		{
			name:           "no allocations",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{},
			want:           5000.00,
		},
		{
			name:           "partial allocation",
			expectedIncome: 5000.00,
			categories: []BudgetCategory{
				{AllocatedAmount: 2000.00},
				{AllocatedAmount: 1500.00},
			},
			want: 1500.00, // 5000 - 3500
		},
		{
			name:           "fully allocated",
			expectedIncome: 5000.00,
			categories: []BudgetCategory{
				{AllocatedAmount: 5000.00},
			},
			want: 0.00,
		},
		{
			name:           "over allocated (negative)",
			expectedIncome: 5000.00,
			categories: []BudgetCategory{
				{AllocatedAmount: 3000.00},
				{AllocatedAmount: 3000.00},
			},
			want: -1000.00, // 5000 - 6000
		},
		{
			name:           "zero expected income zero allocations",
			expectedIncome: 0,
			categories:     []BudgetCategory{},
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				ExpectedIncome: tt.expectedIncome,
				Categories:     tt.categories,
			}
			if got := budget.UnallocatedAmount(); got != tt.want {
				t.Errorf("UnallocatedAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_UnallocatedAmount_UsesExpectedIncome confirms that UnallocatedAmount uses
// ExpectedIncome (not IncomeAmount) as the basis for calculation (Issue #1048).
func TestBudget_UnallocatedAmount_UsesExpectedIncome(t *testing.T) {
	budget := Budget{
		IncomeAmount:   3000.00, // old field — should NOT be used
		ExpectedIncome: 5000.00, // new field — MUST be used
		Categories: []BudgetCategory{
			{AllocatedAmount: 2000.00},
		},
	}
	// Should be 5000 - 2000 = 3000, not 3000 - 2000 = 1000
	want := 3000.00
	if got := budget.UnallocatedAmount(); got != want {
		t.Errorf("UnallocatedAmount() = %v, want %v (must use ExpectedIncome, not IncomeAmount)", got, want)
	}
}

// TestBudget_IsFullyAllocated tests the IsFullyAllocated check for envelope budgeting.
// IsFullyAllocated must use ExpectedIncome (Issue #1048).
func TestBudget_IsFullyAllocated(t *testing.T) {
	tests := []struct {
		name           string
		expectedIncome float64
		categories     []BudgetCategory
		want           bool
	}{
		{
			name:           "not fully allocated",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{{AllocatedAmount: 3000.00}},
			want:           false,
		},
		{
			name:           "exactly fully allocated",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{{AllocatedAmount: 5000.00}},
			want:           true,
		},
		{
			name:           "over allocated",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{{AllocatedAmount: 6000.00}},
			want:           true, // Still considered "fully" allocated
		},
		{
			name:           "zero expected income is always fully allocated",
			expectedIncome: 0,
			categories:     []BudgetCategory{},
			want:           true,
		},
		{
			name:           "zero expected income with allocations",
			expectedIncome: 0,
			categories:     []BudgetCategory{{AllocatedAmount: 100.00}},
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				ExpectedIncome: tt.expectedIncome,
				Categories:     tt.categories,
			}
			if got := budget.IsFullyAllocated(); got != tt.want {
				t.Errorf("IsFullyAllocated() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_ExceedsExpectedIncome tests the ExceedsExpectedIncome helper (Issue #1048).
func TestBudget_ExceedsExpectedIncome(t *testing.T) {
	tests := []struct {
		name           string
		expectedIncome float64
		categories     []BudgetCategory
		want           bool
	}{
		{
			name:           "total allocated below expected income",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{{AllocatedAmount: 4000.00}},
			want:           false,
		},
		{
			name:           "total allocated exactly equals expected income",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{{AllocatedAmount: 5000.00}},
			want:           false,
		},
		{
			name:           "total allocated exceeds expected income",
			expectedIncome: 5000.00,
			categories: []BudgetCategory{
				{AllocatedAmount: 3000.00},
				{AllocatedAmount: 3000.00},
			},
			want: true,
		},
		{
			name:           "multiple categories under expected income",
			expectedIncome: 5000.00,
			categories: []BudgetCategory{
				{AllocatedAmount: 1000.00},
				{AllocatedAmount: 2000.00},
				{AllocatedAmount: 1500.00},
			},
			want: false, // 4500 < 5000
		},
		{
			name:           "no categories never exceeds",
			expectedIncome: 5000.00,
			categories:     []BudgetCategory{},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := Budget{
				ExpectedIncome: tt.expectedIncome,
				Categories:     tt.categories,
			}
			if got := budget.ExceedsExpectedIncome(); got != tt.want {
				t.Errorf("ExceedsExpectedIncome() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_ExceedsExpectedIncome_Zero verifies no division/panic when ExpectedIncome is 0 (Issue #1048).
func TestBudget_ExceedsExpectedIncome_Zero(t *testing.T) {
	tests := []struct {
		name           string
		expectedIncome float64
		categories     []BudgetCategory
		want           bool
	}{
		{
			name:           "zero expected income no allocations",
			expectedIncome: 0,
			categories:     []BudgetCategory{},
			want:           false,
		},
		{
			name:           "zero expected income with allocations",
			expectedIncome: 0,
			categories:     []BudgetCategory{{AllocatedAmount: 100.00}},
			want:           true, // 100 > 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Must not panic
			budget := Budget{
				ExpectedIncome: tt.expectedIncome,
				Categories:     tt.categories,
			}
			if got := budget.ExceedsExpectedIncome(); got != tt.want {
				t.Errorf("ExceedsExpectedIncome() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBudget_Validate_IncomeFields tests validation of new income fields
func TestBudget_Validate_IncomeFields(t *testing.T) {
	validStartDate := time.Now()

	tests := []struct {
		name    string
		budget  Budget
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid budget with income",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
				IncomeAmount:   5000.00,
				IncomeSources: []IncomeSource{
					{Type: IncomeSourceTypePlaidCategory, Value: "Payroll"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid budget without income (backward compatible)",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "zero income is valid",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
				IncomeAmount:   0,
			},
			wantErr: false,
		},
		{
			name: "negative income_amount",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
				IncomeAmount:   -100.00,
			},
			wantErr: true,
			errMsg:  "income_amount cannot be negative",
		},
		{
			name: "invalid income source in array",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
				IncomeAmount:   5000.00,
				IncomeSources: []IncomeSource{
					{Type: IncomeSourceType("invalid"), Value: "test"},
				},
			},
			wantErr: true,
			errMsg:  "income_sources[0]",
		},
		{
			name: "empty income sources array is valid",
			budget: Budget{
				UserID:         "user-123",
				Name:           "Monthly Budget",
				Period:         BudgetPeriodMonthly,
				StartDate:      validStartDate,
				Amount:         500.00,
				ExpectedIncome: 500.00,
				CurrencyCode:   "USD",
				IncomeAmount:   5000.00,
				IncomeSources:  []IncomeSource{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.budget.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestBudgetCategory_Validate_MatchLogic tests validation of match_logic and tags fields
func TestBudgetCategory_Validate_MatchLogic(t *testing.T) {
	tests := []struct {
		name     string
		category BudgetCategory
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid category_only (default)",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicCategoryOnly,
			},
			wantErr: false,
		},
		{
			name: "valid empty match_logic defaults to category_only",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      "", // Empty is valid, defaults to category_only
			},
			wantErr: false,
		},
		{
			name: "valid tag_only with tags",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{}, // Can be empty for tag_only
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicTagOnly,
				Tags:            []string{"groceries", "food"},
			},
			wantErr: false,
		},
		{
			name: "valid 'and' logic with both category and tags",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicAnd,
				Tags:            []string{"essential"},
			},
			wantErr: false,
		},
		{
			name: "valid 'or' logic with both category and tags",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicOr,
				Tags:            []string{"groceries"},
			},
			wantErr: false,
		},
		{
			name: "invalid match_logic value",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogic("invalid"),
			},
			wantErr: true,
			errMsg:  "invalid match_logic value",
		},
		{
			name: "tag_only without tags",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicTagOnly,
				Tags:            []string{},
			},
			wantErr: true,
			errMsg:  "tags are required when match_logic is tag_only or and",
		},
		{
			name: "'and' logic without tags",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicAnd,
				Tags:            []string{},
			},
			wantErr: true,
			errMsg:  "tags are required when match_logic is tag_only or and",
		},
		{
			name: "category_only with tags is valid (tags ignored)",
			category: BudgetCategory{
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 100.00,
				MatchLogic:      MatchLogicCategoryOnly,
				Tags:            []string{"groceries"}, // Tags present but match_logic is category_only
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.category.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestBudget_JSONMarshaling_WithIncomeFields tests JSON serialization with new envelope budgeting fields
func TestBudget_JSONMarshaling_WithIncomeFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := &Budget{
		ID:           "budget-123",
		UserID:       "user-456",
		Name:         "Monthly Envelope",
		Period:       BudgetPeriodMonthly,
		StartDate:    now,
		Amount:       500.00,
		CurrencyCode: "USD",
		IsActive:     true,
		IncomeAmount: 5000.00,
		IncomeSources: []IncomeSource{
			{Type: IncomeSourceTypePlaidCategory, Value: "Payroll", Description: "Main job"},
			{Type: IncomeSourceTypeTag, Value: "freelance"},
		},
		Categories: []BudgetCategory{
			{
				ID:              "cat-1",
				BudgetID:        "budget-123",
				Category:        []string{"Food & Drink"},
				AllocatedAmount: 500.00,
				Tags:            []string{"groceries", "essential"},
				MatchLogic:      MatchLogicOr,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Budget: %v", err)
	}

	// Unmarshal back
	var unmarshaled Budget
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal Budget: %v", err)
	}

	// Verify new envelope budgeting fields
	if unmarshaled.IncomeAmount != original.IncomeAmount {
		t.Errorf("IncomeAmount mismatch: got %v, want %v", unmarshaled.IncomeAmount, original.IncomeAmount)
	}
	if len(unmarshaled.IncomeSources) != len(original.IncomeSources) {
		t.Errorf("IncomeSources length mismatch: got %v, want %v", len(unmarshaled.IncomeSources), len(original.IncomeSources))
	}
	if len(unmarshaled.IncomeSources) > 0 {
		if unmarshaled.IncomeSources[0].Type != original.IncomeSources[0].Type {
			t.Errorf("IncomeSources[0].Type mismatch: got %v, want %v", unmarshaled.IncomeSources[0].Type, original.IncomeSources[0].Type)
		}
		if unmarshaled.IncomeSources[0].Value != original.IncomeSources[0].Value {
			t.Errorf("IncomeSources[0].Value mismatch: got %v, want %v", unmarshaled.IncomeSources[0].Value, original.IncomeSources[0].Value)
		}
	}

	// Verify category with new fields
	if len(unmarshaled.Categories) > 0 {
		cat := unmarshaled.Categories[0]
		origCat := original.Categories[0]
		if len(cat.Tags) != len(origCat.Tags) {
			t.Errorf("Category.Tags length mismatch: got %v, want %v", len(cat.Tags), len(origCat.Tags))
		}
		if cat.MatchLogic != origCat.MatchLogic {
			t.Errorf("Category.MatchLogic mismatch: got %v, want %v", cat.MatchLogic, origCat.MatchLogic)
		}
	}
}

// Note: contains helper is defined in linked_item_test.go
