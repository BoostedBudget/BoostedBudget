package operations

// Tests for the extended GetPortfolioAllocationBreakdown operation (#1296).
// These tests were written RED-first before the implementation existed.
//
// New query params:
//   - include  (csv: "cash", "liabilities") — default empty → investment-only
//   - group_by ("security_type" [default] | "asset_class" | "account")
//   - basis    ("assets" [default] | "net")

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidGroupBy_Returns422
// verifies that an unrecognised group_by value is rejected with HTTP 422.
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidGroupBy_Returns422(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		GroupBy:            "invalid_group",
	}

	_, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.Error(t, err, "unknown group_by must return an error")
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
			"invalid group_by must return 422 Unprocessable Entity")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidBasis_Returns422
// verifies that an unrecognised basis value is rejected with HTTP 422.
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidBasis_Returns422(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Basis:              "invalid_basis",
	}

	_, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.Error(t, err, "unknown basis must return an error")
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
			"invalid basis must return 422 Unprocessable Entity")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidIncludeValue_Returns422
// verifies that an unrecognised include value (not "cash" or "liabilities") is rejected.
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_InvalidIncludeValue_Returns422(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Include:            "unknown_type",
	}

	_, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.Error(t, err, "unknown include value must return an error")
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus(),
			"invalid include value must return 422 Unprocessable Entity")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_DefaultParams_BackwardCompat
// verifies that omitting all new params works exactly as before — investments only.
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_DefaultParams_BackwardCompat(t *testing.T) {
	alloc := &domain.AllocationBreakdown{
		TotalValue: 3000.0,
		Buckets:    []domain.AllocationItem{{SecurityType: "stock", TotalValue: 3000.0, Percentage: 100.0}},
	}
	analytSvc := &mockInvestmentAnalyticsService{allocation: alloc}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		// No Include, GroupBy, or Basis — all defaults.
	}

	out, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 3000.0, out.Body.TotalValue)
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_WithCash_PassesOptionsToService
// verifies that include=cash is accepted, parsed into AllocationOptions, and the service
// is called (the mock always returns the canned result; what matters here is no validation error).
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_WithCash_PassesOptionsToService(t *testing.T) {
	alloc := &domain.AllocationBreakdown{
		TotalValue: 8000.0,
		Buckets: []domain.AllocationItem{
			{SecurityType: "stock", TotalValue: 5000.0, Percentage: 62.5},
			{SecurityType: "cash", TotalValue: 3000.0, Percentage: 37.5},
		},
	}
	analytSvc := &mockInvestmentAnalyticsService{allocation: alloc}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Include:            "cash",
		GroupBy:            "security_type",
		Basis:              "assets",
	}

	out, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 8000.0, out.Body.TotalValue)
	assert.Len(t, out.Body.Buckets, 2)
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_AssetClassGroupBy_Valid
// verifies that group_by=asset_class is a valid value (not rejected).
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_AssetClassGroupBy_Valid(t *testing.T) {
	alloc := &domain.AllocationBreakdown{TotalValue: 5000.0, Buckets: []domain.AllocationItem{}}
	analytSvc := &mockInvestmentAnalyticsService{allocation: alloc}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		GroupBy:            "asset_class",
	}

	_, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)
	assert.NoError(t, err, "group_by=asset_class must be a valid value")
}

// TestInvestmentHandler_GetPortfolioAllocationBreakdown_NetBasis_Valid
// verifies that basis=net is a valid value (not rejected).
func TestInvestmentHandler_GetPortfolioAllocationBreakdown_NetBasis_Valid(t *testing.T) {
	alloc := &domain.AllocationBreakdown{TotalValue: 5000.0, Buckets: []domain.AllocationItem{}}
	analytSvc := &mockInvestmentAnalyticsService{allocation: alloc}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Basis:              "net",
	}

	_, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)
	assert.NoError(t, err, "basis=net must be a valid value")
}
