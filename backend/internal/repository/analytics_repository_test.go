package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestEffectiveCategoryExpr_IncludesUncategorizedFallback verifies that the category_token
// grouping expression falls back to 'UNCATEGORIZED' when no category is present.
// The PFCv2 discriminated-union model means custom kind uses 'custom:<id>' and pfc kind
// uses the PFC column values. When none match, COALESCE provides 'UNCATEGORIZED'.
func TestEffectiveCategoryExpr_IncludesUncategorizedFallback(t *testing.T) {
	expr := effectiveCategoryExpr(true)
	if !contains(expr, "COALESCE") {
		t.Errorf("expr must use COALESCE for fallback: %s", expr)
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

// TestEffectiveCategoryDisplayExpr_UsesEffectiveCategoryNameColumn verifies that the
// display expression reads from the denormalized effective_category_name column
// instead of performing a JOIN on custom_categories.
func TestEffectiveCategoryDisplayExpr_UsesEffectiveCategoryNameColumn(t *testing.T) {
	expr := effectiveCategoryDisplayExpr()
	// Must reference the denormalized column directly
	if !contains(expr, "effective_category_name") {
		t.Errorf("display expr must reference effective_category_name column, got: %s", expr)
	}
	// Must NOT require a JOIN on custom_categories (no cc. alias)
	if contains(expr, "cc.name") {
		t.Errorf("display expr must NOT reference cc.name (JOIN removed), got: %s", expr)
	}
	if contains(expr, "category_override_kind") {
		t.Errorf("display expr must NOT branch on category_override_kind (column is pre-computed), got: %s", expr)
	}
}

// TestEffectiveTransferExpr_UsesEffectivePfcPrimaryColumn verifies that the transfer
// filter expression reads from the denormalized effective_pfc_primary column.
func TestEffectiveTransferExpr_UsesEffectivePfcPrimaryColumn(t *testing.T) {
	expr := effectiveTransferExpr()
	// Must reference the denormalized column
	if !contains(expr, "effective_pfc_primary") {
		t.Errorf("transfer expr must reference effective_pfc_primary column, got: %s", expr)
	}
	// Must NOT use the old CASE/override pattern
	if contains(expr, "pfc_primary_override") {
		t.Errorf("transfer expr must NOT reference pfc_primary_override directly, got: %s", expr)
	}
	if contains(expr, "category_override_kind") {
		t.Errorf("transfer expr must NOT branch on category_override_kind (column is pre-computed), got: %s", expr)
	}
}

// TestBuildSpendingFilter_CustomCategoryRollup verifies the custom-category scope
// clause and roll-up behavior (Issue #1203).
func TestBuildSpendingFilter_CustomCategoryRollup(t *testing.T) {
	base := []interface{}{"user-1", time.Now(), time.Now()}

	t.Run("rollup includes children via parent_id", func(t *testing.T) {
		filters := domain.SpendingFilters{
			IncludeTransfers:  true, // suppress the transfer clause for a focused assertion
			CustomCategoryIDs: []string{"id1", "id2"},
			RollUpChildren:    true,
		}
		clause, args, _ := buildSpendingFilter(filters, base)
		if !contains(clause, "custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1") {
			t.Errorf("expected rollup subquery, got: %s", clause)
		}
		if !contains(clause, "parent_id::text IN") {
			t.Errorf("expected parent_id rollup, got: %s", clause)
		}
		if len(args) != 5 { // 3 base + 2 ids
			t.Errorf("expected 5 args, got %d", len(args))
		}
	})

	t.Run("no rollup matches exact ids only, namespaced to requester (#1369 gap-closure G4)", func(t *testing.T) {
		filters := domain.SpendingFilters{
			IncludeTransfers:  true,
			CustomCategoryIDs: []string{"id1"},
			RollUpChildren:    false,
		}
		clause, _, _ := buildSpendingFilter(filters, base)
		if !contains(clause, "custom_category_override_id IN (SELECT id FROM custom_categories WHERE user_id = $1::uuid AND id::text IN") {
			t.Errorf("expected exact-id match namespaced to requester's own custom categories, got: %s", clause)
		}
		if contains(clause, "parent_id::text IN") {
			t.Errorf("did not expect rollup subquery, got: %s", clause)
		}
	})
}

// ─── #1626 Phase 3, Defect 1: the fill horizon must not fabricate the future ────────────────
//
// fillMissingPeriods rebuilds its output SOLELY from a generated key sequence, so a real SQL
// row whose period key falls outside that sequence used to be DISCARDED rather than merely left
// unfilled. That is what makes the obvious fix — clamp the generated horizon to "now" — a
// data-DELETING change: a user whose institution's calendar date is ahead of UTC has real,
// already-returned spending for a period the clamp would drop. The horizon is therefore
// max(caller's end, last period key actually present in the data), and only FABRICATED buckets
// are clamped.

// TestFillMissingPeriods_KeepsEveryRealPeriodBeyondTheHorizon is the assertion that catches the
// bad fix: without it, a naive clamp passes every other test in this file.
func TestFillMissingPeriods_KeepsEveryRealPeriodBeyondTheHorizon(t *testing.T) {
	loc := time.UTC
	realBeyond := domain.CashFlowGroupedData{
		PeriodStart:      time.Date(2024, 3, 7, 0, 0, 0, 0, loc),
		Income:           0,
		Expenses:         42.5,
		TransactionCount: 1,
	}
	data := []domain.CashFlowGroupedData{
		{PeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, loc), Income: 100, Expenses: 50, TransactionCount: 5},
		realBeyond,
	}

	result := fillMissingPeriods(data, time.Date(2024, 3, 1, 0, 0, 0, 0, loc), time.Date(2024, 3, 5, 0, 0, 0, 0, loc), "daily")

	// March 1..5 generated, plus the real March 7 row that lies beyond the horizon.
	require.Len(t, result, 6)
	found := false
	for _, p := range result {
		if p.PeriodStart.Equal(realBeyond.PeriodStart) {
			found = true
			assert.Equal(t, realBeyond, p, "the real row must survive with its own values, not zeroed")
		}
	}
	assert.True(t, found, "a period present in the SQL result was DROPPED by the fill: %+v", result)

	// Output stays ordered by period start — the chart plots it in array order.
	for i := 1; i < len(result); i++ {
		assert.True(t, result[i-1].PeriodStart.Before(result[i].PeriodStart),
			"periods out of order at index %d: %v then %v", i, result[i-1].PeriodStart, result[i].PeriodStart)
	}
}

// TestFillMissingPeriods_FabricatesNothingWhenTheWholeRangeIsBeyondTheHorizon covers the
// truncateDate trap: truncateDate moves a start date BACKWARD to Monday/month-start, so a range
// lying entirely beyond the horizon (start Friday, horizon the preceding Wednesday) still
// truncated to a Monday inside the horizon and emitted one fake "current period" bucket.
func TestFillMissingPeriods_FabricatesNothingWhenTheWholeRangeIsBeyondTheHorizon(t *testing.T) {
	loc := time.UTC
	tests := []struct {
		name      string
		startDate time.Time
		horizon   time.Time
		grouping  string
	}{
		{
			name:      "weekly - start Friday, horizon the preceding Wednesday",
			startDate: time.Date(2024, 3, 8, 0, 0, 0, 0, loc), // Friday
			horizon:   time.Date(2024, 3, 6, 0, 0, 0, 0, loc), // Wednesday of the same week
			grouping:  "weekly",
		},
		{
			name:      "monthly - start late in the month, horizon earlier in the same month",
			startDate: time.Date(2024, 3, 20, 0, 0, 0, 0, loc),
			horizon:   time.Date(2024, 3, 6, 0, 0, 0, 0, loc),
			grouping:  "monthly",
		},
		{
			name:      "daily - start after the horizon",
			startDate: time.Date(2024, 3, 8, 0, 0, 0, 0, loc),
			horizon:   time.Date(2024, 3, 6, 0, 0, 0, 0, loc),
			grouping:  "daily",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (no data)", func(t *testing.T) {
			assert.Empty(t, fillMissingPeriods(nil, tt.startDate, tt.horizon, tt.grouping))
			assert.Empty(t, generateEmptyPeriods(tt.startDate, tt.horizon, tt.grouping))
		})

		t.Run(tt.name+" (a real row at the truncated bucket start survives)", func(t *testing.T) {
			real := domain.CashFlowGroupedData{
				PeriodStart:      truncateDate(tt.startDate, tt.grouping),
				Income:           0,
				Expenses:         12,
				TransactionCount: 1,
			}
			// A real row is never suppressed, even when its bucket start precedes the horizon
			// only because truncateDate moved it there.
			result := fillMissingPeriods([]domain.CashFlowGroupedData{real}, tt.startDate, tt.horizon, tt.grouping)
			require.Len(t, result, 1)
			assert.Equal(t, real, result[0])
		})

		// The row here sits one period AFTER the truncated start, which is what makes this case
		// able to fail: with the real row at the truncated start, suppressing the generated grid
		// and generating it both produce the same single bucket, so a mutation that drops the
		// guard survives. This subtest was added because that mutation DID survive.
		t.Run(tt.name+" (a real row beyond the horizon licenses no fabricated bucket)", func(t *testing.T) {
			real := domain.CashFlowGroupedData{
				PeriodStart:      advancePeriod(truncateDate(tt.startDate, tt.grouping), tt.grouping),
				Income:           0,
				Expenses:         12,
				TransactionCount: 1,
			}
			result := fillMissingPeriods([]domain.CashFlowGroupedData{real}, tt.startDate, tt.horizon, tt.grouping)
			require.Len(t, result, 1, "fabricated a bucket for a range lying entirely beyond the horizon: %+v", result)
			assert.Equal(t, real, result[0])
		})
	}
}

// TestFillMissingPeriods_GeneratesNoEmptyBucketPastTheHorizon is assertion (a): the horizon may
// be stretched by a real row, but that must not license a fabricated bucket after it.
func TestFillMissingPeriods_GeneratesNoEmptyBucketPastTheHorizon(t *testing.T) {
	loc := time.UTC
	horizon := time.Date(2024, 3, 5, 0, 0, 0, 0, loc)
	data := []domain.CashFlowGroupedData{
		{PeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, loc), Income: 100, Expenses: 50, TransactionCount: 5},
		{PeriodStart: time.Date(2024, 3, 10, 0, 0, 0, 0, loc), Income: 7, Expenses: 0, TransactionCount: 1},
	}

	result := fillMissingPeriods(data, time.Date(2024, 3, 1, 0, 0, 0, 0, loc), horizon, "daily")

	for _, p := range result {
		if p.TransactionCount == 0 && p.Income == 0 && p.Expenses == 0 {
			assert.False(t, p.PeriodStart.After(horizon),
				"fabricated an empty bucket at %v, past the horizon %v", p.PeriodStart, horizon)
		}
	}
	// March 1..5 plus the real March 10 row: the gap between them is NOT bridged with zeroes.
	assert.Len(t, result, 6)
}
