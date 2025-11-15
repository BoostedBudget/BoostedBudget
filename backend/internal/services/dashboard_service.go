package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// DashboardService provides balance dashboard functionality
// Issue #353: Balance Tracking Dashboard
type DashboardService interface {
	// GetBalanceSummary returns current balance overview with breakdown by account type
	GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error)

	// GetBalanceHistory returns time series balance data for charts
	GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error)

	// GetAccountTypes returns available account types for the user
	GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error)
}

type dashboardService struct {
	balanceSnapshotRepo repository.BalanceSnapshotRepository
	accountRepo         repository.AccountRepository
}

// NewDashboardService creates a new DashboardService instance
func NewDashboardService(
	balanceSnapshotRepo repository.BalanceSnapshotRepository,
	accountRepo repository.AccountRepository,
) DashboardService {
	return &dashboardService{
		balanceSnapshotRepo: balanceSnapshotRepo,
		accountRepo:         accountRepo,
	}
}

// GetBalanceSummary returns current balance overview with breakdown by account type
func (s *dashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	// Determine the date for balance snapshot
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters != nil && filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// Get latest snapshots for each account
	snapshots, err := s.balanceSnapshotRepo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to account details
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	// Build account map for lookups
	accountMap := make(map[string]domain.Account)
	for _, account := range accounts {
		accountMap[account.ID] = account
	}

	// Group snapshots by account type
	byType := make(map[string]*domain.AccountTypeBalance)
	var totalBalance float64

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshot.AccountID]
		if !exists {
			continue // Skip if account not found
		}

		// Apply account type filter if specified
		if filters != nil && len(filters.AccountTypes) > 0 {
			typeMatch := false
			for _, filterType := range filters.AccountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue // Skip if account type doesn't match filter
			}
		}

		// Get current balance from snapshot
		balance := 0.0
		if snapshot.CurrentBalance != nil {
			balance = *snapshot.CurrentBalance
		}

		// Initialize type balance if not exists
		if _, exists := byType[account.Type]; !exists {
			byType[account.Type] = &domain.AccountTypeBalance{
				Balance:      0,
				AccountCount: 0,
				Accounts:     []domain.AccountBalanceDetail{},
			}
		}

		// Add to type aggregation
		typeBalance := byType[account.Type]
		typeBalance.Balance += balance
		typeBalance.AccountCount++
		typeBalance.Accounts = append(typeBalance.Accounts, domain.AccountBalanceDetail{
			ID:      account.ID,
			Name:    account.Name,
			Balance: balance,
		})

		totalBalance += balance
	}

	// Calculate period comparison (vs 30 days ago)
	var periodComparison *domain.PeriodComparisonData
	previousDate := asOfDate.AddDate(0, 0, -30)

	previousSnapshots, err := s.balanceSnapshotRepo.GetByUserIDAndDateRange(ctx, userID, previousDate, previousDate)
	if err == nil && len(previousSnapshots) > 0 {
		var previousBalance float64
		for _, snapshot := range previousSnapshots {
			if snapshot.CurrentBalance != nil {
				previousBalance += *snapshot.CurrentBalance
			}
		}

		changeAmount := totalBalance - previousBalance
		changePercent := 0.0
		if previousBalance != 0 {
			changePercent = (changeAmount / previousBalance) * 100
		}

		periodComparison = &domain.PeriodComparisonData{
			PreviousBalance: previousBalance,
			ChangeAmount:    changeAmount,
			ChangePercent:   changePercent,
		}
	}

	return &domain.BalanceSummaryResponse{
		AsOfDate:         asOfDate,
		TotalBalance:     totalBalance,
		ByType:           byType,
		PeriodComparison: periodComparison,
	}, nil
}

// GetBalanceHistory returns time series balance data for charts
func (s *dashboardService) GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error) {
	// Default to last 30 days if not specified
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -30)

	if filters != nil {
		if filters.StartDate != nil {
			startDate = filters.StartDate.Truncate(24 * time.Hour)
		}
		if filters.EndDate != nil {
			endDate = filters.EndDate.Truncate(24 * time.Hour)
		}
	}

	// Validate date range
	if startDate.After(endDate) {
		return nil, fmt.Errorf("start_date must be before or equal to end_date")
	}

	// Get account types to filter (if specified)
	var accountTypes []string
	if filters != nil && len(filters.AccountTypes) > 0 {
		accountTypes = filters.AccountTypes
	}

	// Get aggregated balance history from repository
	history, err := s.balanceSnapshotRepo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance history: %w", err)
	}

	// Convert to response format
	snapshots := make([]domain.BalanceHistorySnapshot, len(history))
	for i, point := range history {
		snapshots[i] = domain.BalanceHistorySnapshot{
			Date:    point.Date,
			Balance: point.TotalBalance,
			ByType:  point.ByType,
		}
	}

	granularity := "daily"
	if filters != nil && filters.Granularity != "" {
		granularity = filters.Granularity
	}

	return &domain.BalanceHistoryResponse{
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: granularity,
		Snapshots:   snapshots,
	}, nil
}

// GetAccountTypes returns available account types for the user
func (s *dashboardService) GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error) {
	// Get latest snapshots for each account
	snapshots, err := s.balanceSnapshotRepo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to types
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	// Build account map for lookups
	accountMap := make(map[string]domain.Account)
	for _, account := range accounts {
		accountMap[account.ID] = account
	}

	// Aggregate by account type
	typeMap := make(map[string]*domain.AccountTypeInfo)

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshot.AccountID]
		if !exists {
			continue
		}

		// Initialize type info if not exists
		if _, exists := typeMap[account.Type]; !exists {
			typeMap[account.Type] = &domain.AccountTypeInfo{
				Key:          account.Type,
				Label:        formatAccountTypeLabel(account.Type),
				Count:        0,
				TotalBalance: 0,
			}
		}

		typeInfo := typeMap[account.Type]
		typeInfo.Count++

		if snapshot.CurrentBalance != nil {
			typeInfo.TotalBalance += *snapshot.CurrentBalance
		}
	}

	// Convert map to slice
	types := make([]domain.AccountTypeInfo, 0, len(typeMap))
	for _, typeInfo := range typeMap {
		types = append(types, *typeInfo)
	}

	return &domain.AccountTypesResponse{
		Types: types,
	}, nil
}

// formatAccountTypeLabel converts account type keys to user-friendly labels
func formatAccountTypeLabel(accountType string) string {
	labels := map[string]string{
		"investment": "Investments",
		"depository": "Checking & Savings",
		"credit":     "Credit Cards",
		"loan":       "Loans",
		"other":      "Other",
	}

	if label, exists := labels[accountType]; exists {
		return label
	}
	return accountType
}
