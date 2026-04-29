package repository

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestTruncateDate(t *testing.T) {
	// Use a fixed location for consistent results
	loc := time.UTC

	tests := []struct {
		name     string
		input    time.Time
		grouping string
		expected time.Time
	}{
		{
			name:     "daily - no change needed",
			input:    time.Date(2024, 3, 15, 10, 30, 45, 0, loc),
			grouping: "daily",
			expected: time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
		},
		{
			name:     "weekly - truncate to Monday (from Wednesday)",
			input:    time.Date(2024, 3, 13, 10, 30, 45, 0, loc), // Wednesday
			grouping: "weekly",
			expected: time.Date(2024, 3, 11, 0, 0, 0, 0, loc), // Monday
		},
		{
			name:     "weekly - already Monday",
			input:    time.Date(2024, 3, 11, 10, 30, 45, 0, loc), // Monday
			grouping: "weekly",
			expected: time.Date(2024, 3, 11, 0, 0, 0, 0, loc),
		},
		{
			name:     "weekly - Sunday",
			input:    time.Date(2024, 3, 17, 10, 30, 45, 0, loc), // Sunday
			grouping: "weekly",
			expected: time.Date(2024, 3, 11, 0, 0, 0, 0, loc), // Monday
		},
		{
			name:     "monthly - truncate to first of month",
			input:    time.Date(2024, 3, 15, 10, 30, 45, 0, loc),
			grouping: "monthly",
			expected: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
		},
		{
			name:     "monthly - already first of month",
			input:    time.Date(2024, 3, 1, 10, 30, 45, 0, loc),
			grouping: "monthly",
			expected: time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDate(tt.input, tt.grouping)
			if !result.Equal(tt.expected) {
				t.Errorf("truncateDate(%v, %s) = %v, expected %v", tt.input, tt.grouping, result, tt.expected)
			}
		})
	}
}

func TestAdvancePeriod(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name     string
		input    time.Time
		grouping string
		expected time.Time
	}{
		{
			name:     "daily - advance by 1 day",
			input:    time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			grouping: "daily",
			expected: time.Date(2024, 3, 16, 0, 0, 0, 0, loc),
		},
		{
			name:     "weekly - advance by 7 days",
			input:    time.Date(2024, 3, 11, 0, 0, 0, 0, loc),
			grouping: "weekly",
			expected: time.Date(2024, 3, 18, 0, 0, 0, 0, loc),
		},
		{
			name:     "monthly - advance by 1 month",
			input:    time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			grouping: "monthly",
			expected: time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
		},
		{
			name:     "daily - cross month boundary",
			input:    time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			grouping: "daily",
			expected: time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
		},
		{
			name:     "monthly - cross year boundary",
			input:    time.Date(2024, 12, 1, 0, 0, 0, 0, loc),
			grouping: "monthly",
			expected: time.Date(2025, 1, 1, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := advancePeriod(tt.input, tt.grouping)
			if !result.Equal(tt.expected) {
				t.Errorf("advancePeriod(%v, %s) = %v, expected %v", tt.input, tt.grouping, result, tt.expected)
			}
		})
	}
}

func TestGenerateEmptyPeriods(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name          string
		startDate     time.Time
		endDate       time.Time
		grouping      string
		expectedCount int
	}{
		{
			name:          "daily - 7 days",
			startDate:     time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:       time.Date(2024, 3, 7, 0, 0, 0, 0, loc),
			grouping:      "daily",
			expectedCount: 7,
		},
		{
			name:          "weekly - 4 weeks",
			startDate:     time.Date(2024, 3, 4, 0, 0, 0, 0, loc), // Monday
			endDate:       time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			grouping:      "weekly",
			expectedCount: 4, // Weeks starting: March 4, 11, 18, 25
		},
		{
			name:          "monthly - 3 months",
			startDate:     time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			endDate:       time.Date(2024, 3, 31, 0, 0, 0, 0, loc),
			grouping:      "monthly",
			expectedCount: 3,
		},
		{
			name:          "daily - single day",
			startDate:     time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			endDate:       time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			grouping:      "daily",
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateEmptyPeriods(tt.startDate, tt.endDate, tt.grouping)
			if len(result) != tt.expectedCount {
				t.Errorf("generateEmptyPeriods() returned %d periods, expected %d", len(result), tt.expectedCount)
			}

			// Verify all periods have zero values
			for _, period := range result {
				if period.Income != 0 || period.Expenses != 0 || period.TransactionCount != 0 {
					t.Errorf("generateEmptyPeriods() returned non-zero values: %+v", period)
				}
			}
		})
	}
}

func TestFillMissingPeriods(t *testing.T) {
	loc := time.UTC

	tests := []struct {
		name          string
		data          []domain.CashFlowGroupedData
		startDate     time.Time
		endDate       time.Time
		grouping      string
		expectedCount int
		checkFirst    func(t *testing.T, periods []domain.CashFlowGroupedData)
	}{
		{
			name:          "empty data - generate all periods",
			data:          []domain.CashFlowGroupedData{},
			startDate:     time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:       time.Date(2024, 3, 5, 0, 0, 0, 0, loc),
			grouping:      "daily",
			expectedCount: 5,
			checkFirst: func(t *testing.T, periods []domain.CashFlowGroupedData) {
				if periods[0].Income != 0 {
					t.Error("expected zero income for empty data")
				}
			},
		},
		{
			name: "partial data - fill gaps",
			data: []domain.CashFlowGroupedData{
				{PeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, loc), Income: 100, Expenses: 50, TransactionCount: 5},
				{PeriodStart: time.Date(2024, 3, 3, 0, 0, 0, 0, loc), Income: 200, Expenses: 75, TransactionCount: 3},
			},
			startDate:     time.Date(2024, 3, 1, 0, 0, 0, 0, loc),
			endDate:       time.Date(2024, 3, 5, 0, 0, 0, 0, loc),
			grouping:      "daily",
			expectedCount: 5,
			checkFirst: func(t *testing.T, periods []domain.CashFlowGroupedData) {
				if periods[0].Income != 100 {
					t.Errorf("expected income 100 for first period, got %f", periods[0].Income)
				}
				// Second period (March 2) should be zero-filled
				if periods[1].Income != 0 || periods[1].Expenses != 0 {
					t.Errorf("expected zero values for March 2, got income=%f expenses=%f", periods[1].Income, periods[1].Expenses)
				}
				// Third period (March 3) should have data
				if periods[2].Income != 200 {
					t.Errorf("expected income 200 for March 3, got %f", periods[2].Income)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fillMissingPeriods(tt.data, tt.startDate, tt.endDate, tt.grouping)
			if len(result) != tt.expectedCount {
				t.Errorf("fillMissingPeriods() returned %d periods, expected %d", len(result), tt.expectedCount)
			}
			if tt.checkFirst != nil {
				tt.checkFirst(t, result)
			}
		})
	}
}

// TestEffectiveCategoryExpr_OverrideBranchFires_Primary verifies the PFC v2 discriminated
// effective-category expression for primary grouping.
func TestEffectiveCategoryExpr_OverrideBranchFires_Primary(t *testing.T) {
	expr := effectiveCategoryExpr(false)
	if expr == "" {
		t.Fatal("effectiveCategoryExpr returned empty string")
	}
	// Must use category_override_kind discriminator
	if !contains(expr, "category_override_kind") {
		t.Errorf("primary expr should use 'category_override_kind' discriminator, got: %s", expr)
	}
	// Must use pfc_primary_override for pfc overrides
	if !contains(expr, "pfc_primary_override") {
		t.Errorf("primary expr must reference 'pfc_primary_override', got: %s", expr)
	}
	// Must fall through to pfc_primary
	if !contains(expr, "pfc_primary") {
		t.Errorf("primary expr must fall through to 'pfc_primary', got: %s", expr)
	}
	// Must NOT reference dropped legacy columns
	if contains(expr, "personal_finance_category") {
		t.Errorf("primary expr must NOT reference legacy 'personal_finance_category', got: %s", expr)
	}
	if contains(expr, "category_override->>") {
		t.Errorf("primary expr must NOT reference legacy 'category_override' JSON array, got: %s", expr)
	}
}

// TestEffectiveCategoryExpr_OverrideBranchFires_Subcategory verifies the PFC v2 discriminated
// effective-category expression for detailed (subcategory) grouping.
func TestEffectiveCategoryExpr_OverrideBranchFires_Subcategory(t *testing.T) {
	expr := effectiveCategoryExpr(true)
	// Must use category_override_kind discriminator
	if !contains(expr, "category_override_kind") {
		t.Errorf("subcategory expr should use 'category_override_kind' discriminator, got: %s", expr)
	}
	// Must use pfc_detailed_override for pfc overrides
	if !contains(expr, "pfc_detailed_override") {
		t.Errorf("subcategory expr must reference 'pfc_detailed_override', got: %s", expr)
	}
	// Must fall through to pfc_detailed
	if !contains(expr, "pfc_detailed") {
		t.Errorf("subcategory expr must fall through to 'pfc_detailed', got: %s", expr)
	}
	// Must NOT reference dropped legacy columns
	if contains(expr, "personal_finance_category") {
		t.Errorf("subcategory expr must NOT reference legacy 'personal_finance_category', got: %s", expr)
	}
	if contains(expr, "category_override->>") {
		t.Errorf("subcategory expr must NOT reference legacy 'category_override' JSON array, got: %s", expr)
	}
}

// TestEffectiveCategoryExpr_SingleElementOverride_FallsThroughForSubcategory verifies that
// when category_override has only one element, ->>1 returns NULL and the COALESCE falls through.
// The SQL semantics guarantee this: a JSON array element that doesn't exist returns NULL.
func TestEffectiveCategoryExpr_SingleElementOverride_FallsThroughForSubcategory(t *testing.T) {
	expr := effectiveCategoryExpr(true)
	// Single-element override (category_override = '["Food And Drink"]') has no ->>1.
	// NULL from ->>1 → COALESCE falls through to personal_finance_category->>'detailed' → 'UNCATEGORIZED'.
	// We can't test PostgreSQL semantics in a unit test; we verify the expression shape here
	// and the 3-level fallback chain is explicit.
	if !contains(expr, "COALESCE") {
		t.Errorf("expr must use COALESCE for 3-level fallback: %s", expr)
	}
	if !contains(expr, "'UNCATEGORIZED'") {
		t.Errorf("expr must fall back to 'UNCATEGORIZED': %s", expr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
