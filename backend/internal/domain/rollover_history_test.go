package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolloverStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status RolloverStatus
		want   bool
	}{
		{"pending is valid", RolloverStatusPending, true},
		{"applied is valid", RolloverStatusApplied, true},
		{"rejected is valid", RolloverStatusRejected, true},
		{"invalid status", RolloverStatus("unknown"), false},
		{"empty status", RolloverStatus(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}

func TestRolloverType_IsValid(t *testing.T) {
	tests := []struct {
		name       string
		rollType   RolloverType
		want       bool
	}{
		{"full is valid", RolloverTypeFull, true},
		{"capped is valid", RolloverTypeCapped, true},
		{"percentage is valid", RolloverTypePercentage, true},
		{"invalid type", RolloverType("unknown"), false},
		{"empty type", RolloverType(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.rollType.IsValid())
		})
	}
}

func TestRolloverHistory_Validate(t *testing.T) {
	now := time.Now()
	periodStart := now.AddDate(0, -1, 0)
	periodEnd := now

	validRollover := func() *RolloverHistory {
		return &RolloverHistory{
			ID:                     "rollover-1",
			UserID:                 "user-1",
			BudgetID:               "budget-1",
			SourceBudgetCategoryID: "cat-1",
			Amount:                 100.00,
			RolloverType:           RolloverTypeFull,
			BudgetPeriodStart:      periodStart,
			BudgetPeriodEnd:        periodEnd,
			Status:                 RolloverStatusPending,
			CreatedAt:              now,
		}
	}

	t.Run("valid rollover passes validation", func(t *testing.T) {
		rh := validRollover()
		err := rh.Validate()
		require.NoError(t, err)
	})

	t.Run("valid rollover with target category passes validation", func(t *testing.T) {
		rh := validRollover()
		targetID := "cat-2"
		rh.TargetBudgetCategoryID = &targetID
		err := rh.Validate()
		require.NoError(t, err)
	})

	t.Run("valid rollover with optional rule fields", func(t *testing.T) {
		rh := validRollover()
		ruleID := "rule-1"
		execID := "exec-1"
		rh.RuleID = &ruleID
		rh.RuleExecutionID = &execID
		err := rh.Validate()
		require.NoError(t, err)
	})

	t.Run("missing user_id fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.UserID = ""
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "user_id is required")
	})

	t.Run("missing budget_id fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.BudgetID = ""
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget_id is required")
	})

	t.Run("missing source_budget_category_id fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.SourceBudgetCategoryID = ""
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source_budget_category_id is required")
	})

	t.Run("zero amount fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.Amount = 0
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("negative amount fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.Amount = -50.00
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("invalid rollover_type fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.RolloverType = RolloverType("invalid")
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid rollover_type")
	})

	t.Run("invalid status fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.Status = RolloverStatus("invalid")
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid status")
	})

	t.Run("zero budget_period_start fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.BudgetPeriodStart = time.Time{}
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget_period_start is required")
	})

	t.Run("zero budget_period_end fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.BudgetPeriodEnd = time.Time{}
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget_period_end is required")
	})

	t.Run("period_end before period_start fails validation", func(t *testing.T) {
		rh := validRollover()
		rh.BudgetPeriodStart = now
		rh.BudgetPeriodEnd = now.AddDate(0, -1, 0)
		err := rh.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget_period_end must be after budget_period_start")
	})
}

func TestRolloverHistory_MarkApplied(t *testing.T) {
	rh := &RolloverHistory{
		Status: RolloverStatusPending,
	}

	rh.MarkApplied()

	assert.Equal(t, RolloverStatusApplied, rh.Status)
	require.NotNil(t, rh.AppliedAt)
	assert.WithinDuration(t, time.Now(), *rh.AppliedAt, time.Second)
}

func TestRolloverHistory_MarkRejected(t *testing.T) {
	rh := &RolloverHistory{
		Status: RolloverStatusPending,
	}

	rh.MarkRejected()

	assert.Equal(t, RolloverStatusRejected, rh.Status)
}

func TestRolloverHistory_IsToUnallocated(t *testing.T) {
	t.Run("nil target means unallocated", func(t *testing.T) {
		rh := &RolloverHistory{
			TargetBudgetCategoryID: nil,
		}
		assert.True(t, rh.IsToUnallocated())
	})

	t.Run("non-nil target means specific category", func(t *testing.T) {
		targetID := "cat-2"
		rh := &RolloverHistory{
			TargetBudgetCategoryID: &targetID,
		}
		assert.False(t, rh.IsToUnallocated())
	})
}
