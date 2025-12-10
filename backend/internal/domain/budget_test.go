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
				UserID:       "user-123",
				Name:         "Monthly Groceries",
				Period:       BudgetPeriodMonthly,
				StartDate:    validStartDate,
				Amount:       500.00,
				CurrencyCode: "USD",
			},
			wantErr: false,
		},
		{
			name: "valid custom period budget with end date",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Vacation Budget",
				Period:       BudgetPeriodCustom,
				StartDate:    validStartDate,
				EndDate:      &validEndDate,
				Amount:       2000.00,
				CurrencyCode: "USD",
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
				UserID:    "user-123",
				Name:      "Test Budget",
				Period:    BudgetPeriodMonthly,
				StartDate: validStartDate,
				Amount:    100.00,
			},
			wantErr: true,
			errMsg:  "currency_code is required",
		},
		{
			name: "custom period without end_date",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Custom Budget",
				Period:       BudgetPeriodCustom,
				StartDate:    validStartDate,
				Amount:       100.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "end_date is required for custom period budgets",
		},
		{
			name: "end_date before start_date",
			budget: Budget{
				UserID:       "user-123",
				Name:         "Test Budget",
				Period:       BudgetPeriodCustom,
				StartDate:    validEndDate, // later date as start
				EndDate:      &validStartDate, // earlier date as end
				Amount:       100.00,
				CurrencyCode: "USD",
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
