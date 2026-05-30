package services

// Analytics helper method tests (Issue #1087: MCP analytics starter set)
// RED: These tests will fail until GetCategoryTotalForPeriod and GetTopExpense
// are added to AnalyticsService, AnalyticsRepository, and analyticsService.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestAnalyticsService_GetCategoryTotalForPeriod(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, loc)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)

	t.Run("returns total for known category", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(_ context.Context, _ string, category string, _, _ time.Time) (*domain.CategoryTotalRow, error) {
				if category != "FOOD_AND_DRINK" {
					t.Errorf("unexpected category %q", category)
				}
				return &domain.CategoryTotalRow{Total: 450.0, TransactionCount: 18}, nil
			},
		}
		svc := NewAnalyticsService(mockRepo)
		result, err := svc.GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Total != 450.0 {
			t.Errorf("expected total 450, got %f", result.Total)
		}
		if result.TransactionCount != 18 {
			t.Errorf("expected 18 transactions, got %d", result.TransactionCount)
		}
		if result.Category != "FOOD_AND_DRINK" {
			t.Errorf("expected category FOOD_AND_DRINK in response, got %q", result.Category)
		}
		if result.StartDate != "2024-03-01" {
			t.Errorf("expected start_date 2024-03-01, got %q", result.StartDate)
		}
		if result.EndDate != "2024-03-31" {
			t.Errorf("expected end_date 2024-03-31, got %q", result.EndDate)
		}
	})

	t.Run("returns zero total when no matching transactions", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(_ context.Context, _ string, _ string, _, _ time.Time) (*domain.CategoryTotalRow, error) {
				return &domain.CategoryTotalRow{Total: 0, TransactionCount: 0}, nil
			},
		}
		svc := NewAnalyticsService(mockRepo)
		result, err := svc.GetCategoryTotalForPeriod(ctx, "user-1", "TRAVEL", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Total != 0 {
			t.Errorf("expected 0, got %f", result.Total)
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(_ context.Context, _ string, _ string, _, _ time.Time) (*domain.CategoryTotalRow, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewAnalyticsService(mockRepo)
		_, err := svc.GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", start, end)
		if err == nil {
			t.Error("expected error from repository")
		}
	})

	t.Run("returns error when end date is before start date", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{}
		svc := NewAnalyticsService(mockRepo)
		_, err := svc.GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", end, start)
		if err == nil {
			t.Error("expected error for inverted date range")
		}
	})
}

func TestAnalyticsService_GetTopExpense(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, loc)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)

	t.Run("returns top expense transaction", func(t *testing.T) {
		txDate := time.Date(2024, 3, 15, 0, 0, 0, 0, loc)
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(_ context.Context, _ string, _, _ time.Time) (*domain.TopExpenseRow, error) {
				return &domain.TopExpenseRow{
					TransactionID: "txn-abc",
					MerchantName:  "Amazon",
					Amount:        299.99,
					Category:      "GENERAL_MERCHANDISE",
					Date:          txDate,
				}, nil
			},
		}
		svc := NewAnalyticsService(mockRepo)
		result, err := svc.GetTopExpense(ctx, "user-1", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TransactionID != "txn-abc" {
			t.Errorf("expected txn-abc, got %q", result.TransactionID)
		}
		if result.Amount != 299.99 {
			t.Errorf("expected 299.99, got %f", result.Amount)
		}
		if result.MerchantName != "Amazon" {
			t.Errorf("expected Amazon, got %q", result.MerchantName)
		}
		if result.Date != "2024-03-15" {
			t.Errorf("expected 2024-03-15, got %q", result.Date)
		}
	})

	t.Run("returns nil when no expenses in period", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(_ context.Context, _ string, _, _ time.Time) (*domain.TopExpenseRow, error) {
				return nil, nil
			},
		}
		svc := NewAnalyticsService(mockRepo)
		result, err := svc.GetTopExpense(ctx, "user-1", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result when no expenses, got %+v", result)
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(_ context.Context, _ string, _, _ time.Time) (*domain.TopExpenseRow, error) {
				return nil, errors.New("db error")
			},
		}
		svc := NewAnalyticsService(mockRepo)
		_, err := svc.GetTopExpense(ctx, "user-1", start, end)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}
