package domain

import (
	"testing"
	"time"
)

func TestCashFlowFilters_Validate(t *testing.T) {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)

	tests := []struct {
		name    string
		filters CashFlowFilters
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid filters",
			filters: CashFlowFilters{
				StartDate: yesterday,
				EndDate:   now,
				Grouping:  "daily",
			},
			wantErr: false,
		},
		{
			name: "missing start_date",
			filters: CashFlowFilters{
				EndDate: now,
			},
			wantErr: true,
			errMsg:  "start_date is required",
		},
		{
			name: "missing end_date",
			filters: CashFlowFilters{
				StartDate: yesterday,
			},
			wantErr: true,
			errMsg:  "end_date is required",
		},
		{
			name: "end_date before start_date",
			filters: CashFlowFilters{
				StartDate: now,
				EndDate:   yesterday,
			},
			wantErr: true,
			errMsg:  "end_date cannot be before start_date",
		},
		{
			name: "invalid grouping",
			filters: CashFlowFilters{
				StartDate: yesterday,
				EndDate:   now,
				Grouping:  "yearly",
			},
			wantErr: true,
			errMsg:  "grouping must be one of: daily, weekly, monthly",
		},
		{
			name: "valid grouping - weekly",
			filters: CashFlowFilters{
				StartDate: yesterday,
				EndDate:   now,
				Grouping:  "weekly",
			},
			wantErr: false,
		},
		{
			name: "valid grouping - monthly",
			filters: CashFlowFilters{
				StartDate: yesterday,
				EndDate:   now,
				Grouping:  "monthly",
			},
			wantErr: false,
		},
		{
			name: "empty grouping is valid",
			filters: CashFlowFilters{
				StartDate: yesterday,
				EndDate:   now,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filters.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("CashFlowFilters.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("CashFlowFilters.Validate() error = %v, wantErrMsg %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestCashFlowAggregation_NetCashFlow(t *testing.T) {
	tests := []struct {
		name     string
		agg      CashFlowAggregation
		expected float64
	}{
		{
			name: "positive net cash flow",
			agg: CashFlowAggregation{
				Income:   1000.00,
				Expenses: 600.00,
			},
			expected: 400.00,
		},
		{
			name: "negative net cash flow",
			agg: CashFlowAggregation{
				Income:   500.00,
				Expenses: 800.00,
			},
			expected: -300.00,
		},
		{
			name: "zero net cash flow",
			agg: CashFlowAggregation{
				Income:   750.00,
				Expenses: 750.00,
			},
			expected: 0.00,
		},
		{
			name: "all zeros",
			agg: CashFlowAggregation{
				Income:   0,
				Expenses: 0,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.agg.NetCashFlow()
			if result != tt.expected {
				t.Errorf("NetCashFlow() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
