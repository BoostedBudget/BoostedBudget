package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockBalanceSnapshotRepository is a mock for BalanceSnapshotRepository
type MockBalanceSnapshotRepository struct {
	mock.Mock
}

func (m *MockBalanceSnapshotRepository) Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error {
	args := m.Called(ctx, snapshot)
	return args.Error(0)
}

func (m *MockBalanceSnapshotRepository) GetByUserIDAndDateRange(ctx context.Context, userID string, startDate, endDate time.Time) ([]domain.BalanceSnapshot, error) {
	args := m.Called(ctx, userID, startDate, endDate)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceSnapshot), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) GetAggregatedHistory(ctx context.Context, userID string, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	args := m.Called(ctx, userID, startDate, endDate, accountTypes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceHistoryPoint), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) GetLatestByUserID(ctx context.Context, userID string) ([]domain.BalanceSnapshot, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceSnapshot), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) GetLatestByAccountID(ctx context.Context, accountID string) (*domain.BalanceSnapshot, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BalanceSnapshot), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) DeleteByAccountID(ctx context.Context, accountID string) error {
	args := m.Called(ctx, accountID)
	return args.Error(0)
}

func TestGetBalanceSummary_Success(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Mock snapshots
	balance1 := 1500.00
	balance2 := 2500.00
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snapshot-1",
			AccountID:      "account-1",
			UserID:         userID,
			CurrentBalance: &balance1,
			SnapshotAt:     time.Now().UTC(),
		},
		{
			ID:             "snapshot-2",
			AccountID:      "account-2",
			UserID:         userID,
			CurrentBalance: &balance2,
			SnapshotAt:     time.Now().UTC(),
		},
	}

	// Mock accounts
	accounts := []domain.Account{
		{
			ID:     "account-1",
			UserID: userID,
			Name:   "Checking Account",
			Type:   "depository",
		},
		{
			ID:     "account-2",
			UserID: userID,
			Name:   "Savings Account",
			Type:   "depository",
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)

	// Mock previous snapshots for period comparison
	previousBalance1 := 1400.00
	previousBalance2 := 2400.00
	previousSnapshots := []domain.BalanceSnapshot{
		{
			ID:             "prev-snapshot-1",
			AccountID:      "account-1",
			CurrentBalance: &previousBalance1,
		},
		{
			ID:             "prev-snapshot-2",
			AccountID:      "account-2",
			CurrentBalance: &previousBalance2,
		},
	}
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previousSnapshots, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetBalanceSummary(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 4000.00, result.TotalBalance) // 1500 + 2500
	assert.Contains(t, result.ByType, "depository")
	assert.Equal(t, 4000.00, result.ByType["depository"].Balance)
	assert.Equal(t, 2, result.ByType["depository"].AccountCount)
	assert.Len(t, result.ByType["depository"].Accounts, 2)

	// Check period comparison
	assert.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 3800.00, result.PeriodComparison.PreviousBalance) // 1400 + 2400
	assert.Equal(t, 200.00, result.PeriodComparison.ChangeAmount)     // 4000 - 3800
	assert.InDelta(t, 5.26, result.PeriodComparison.ChangePercent, 0.01) // (200/3800)*100

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetBalanceSummary_WithAccountTypeFilter(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Mock snapshots
	balance1 := 1500.00
	balance2 := -500.00
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snapshot-1",
			AccountID:      "account-1",
			UserID:         userID,
			CurrentBalance: &balance1,
		},
		{
			ID:             "snapshot-2",
			AccountID:      "account-2",
			UserID:         userID,
			CurrentBalance: &balance2,
		},
	}

	// Mock accounts - different types
	accounts := []domain.Account{
		{
			ID:     "account-1",
			UserID: userID,
			Name:   "Checking Account",
			Type:   "depository",
		},
		{
			ID:     "account-2",
			UserID: userID,
			Name:   "Credit Card",
			Type:   "credit",
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute with filter for depository only
	filters := &domain.DashboardFilters{
		AccountTypes: []string{"depository"},
	}
	result, err := service.GetBalanceSummary(ctx, userID, filters)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1500.00, result.TotalBalance) // Only depository account
	assert.Contains(t, result.ByType, "depository")
	assert.NotContains(t, result.ByType, "credit") // Filtered out
	assert.Equal(t, 1, result.ByType["depository"].AccountCount)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetBalanceSummary_SnapshotRepoError(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(nil, assert.AnError)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetBalanceSummary(ctx, userID, nil)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get balance snapshots")

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetBalanceHistory_Success(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Mock history points
	history := []domain.BalanceHistoryPoint{
		{
			Date:         startDate,
			TotalBalance: 3000.00,
			ByType: map[string]float64{
				"depository": 2000.00,
				"credit":     1000.00,
			},
		},
		{
			Date:         startDate.AddDate(0, 0, 3),
			TotalBalance: 3500.00,
			ByType: map[string]float64{
				"depository": 2500.00,
				"credit":     1000.00,
			},
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}
	result, err := service.GetBalanceHistory(ctx, userID, filters)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, startDate, result.StartDate)
	assert.Equal(t, endDate, result.EndDate)
	assert.Equal(t, "daily", result.Granularity)
	assert.Len(t, result.Snapshots, 2)
	assert.Equal(t, 3000.00, result.Snapshots[0].Balance)
	assert.Equal(t, 3500.00, result.Snapshots[1].Balance)
	assert.Equal(t, 2000.00, result.Snapshots[0].ByType["depository"])

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetBalanceHistory_InvalidDateRange(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC()
	endDate := startDate.AddDate(0, 0, -7) // End before start

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}
	result, err := service.GetBalanceHistory(ctx, userID, filters)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "start_date must be before or equal to end_date")
}

func TestGetBalanceHistory_WithAccountTypeFilter(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	accountTypes := []string{"depository", "investment"}

	// Mock history points
	history := []domain.BalanceHistoryPoint{
		{
			Date:         startDate,
			TotalBalance: 5000.00,
			ByType: map[string]float64{
				"depository": 2000.00,
				"investment": 3000.00,
			},
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), accountTypes).Return(history, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate:    &startDate,
		EndDate:      &endDate,
		AccountTypes: accountTypes,
	}
	result, err := service.GetBalanceHistory(ctx, userID, filters)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Snapshots, 1)
	assert.Equal(t, 5000.00, result.Snapshots[0].Balance)
	assert.Len(t, result.Snapshots[0].ByType, 2)

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetAccountTypes_Success(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Mock snapshots
	balance1 := 1500.00
	balance2 := 2500.00
	balance3 := -500.00
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snapshot-1",
			AccountID:      "account-1",
			CurrentBalance: &balance1,
		},
		{
			ID:             "snapshot-2",
			AccountID:      "account-2",
			CurrentBalance: &balance2,
		},
		{
			ID:             "snapshot-3",
			AccountID:      "account-3",
			CurrentBalance: &balance3,
		},
	}

	// Mock accounts
	accounts := []domain.Account{
		{
			ID:   "account-1",
			Name: "Checking",
			Type: "depository",
		},
		{
			ID:   "account-2",
			Name: "Savings",
			Type: "depository",
		},
		{
			ID:   "account-3",
			Name: "Credit Card",
			Type: "credit",
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetAccountTypes(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Types, 2) // depository and credit

	// Find types in result
	var depositoryType, creditType *domain.AccountTypeInfo
	for i := range result.Types {
		if result.Types[i].Key == "depository" {
			depositoryType = &result.Types[i]
		} else if result.Types[i].Key == "credit" {
			creditType = &result.Types[i]
		}
	}

	assert.NotNil(t, depositoryType)
	assert.Equal(t, "Checking & Savings", depositoryType.Label)
	assert.Equal(t, 2, depositoryType.Count)
	assert.Equal(t, 4000.00, depositoryType.TotalBalance) // 1500 + 2500

	assert.NotNil(t, creditType)
	assert.Equal(t, "Credit Cards", creditType.Label)
	assert.Equal(t, 1, creditType.Count)
	assert.Equal(t, -500.00, creditType.TotalBalance)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetAccountTypes_NoSnapshots(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Setup mocks - empty snapshots
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return([]domain.BalanceSnapshot{}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetAccountTypes(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Types, 0) // No types when no snapshots

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// Issue #127: Net Worth Tests

func TestGetNetWorth_Success_MixedAccountTypes(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Mock snapshots - mix of assets and liabilities
	depositoryBalance := 5000.00
	investmentBalance := 10000.00
	creditBalance := -2000.00 // Credit card debt
	loanBalance := -15000.00  // Loan balance

	snapshots := []domain.BalanceSnapshot{
		{ID: "snapshot-1", AccountID: "account-1", UserID: userID, CurrentBalance: &depositoryBalance},
		{ID: "snapshot-2", AccountID: "account-2", UserID: userID, CurrentBalance: &investmentBalance},
		{ID: "snapshot-3", AccountID: "account-3", UserID: userID, CurrentBalance: &creditBalance},
		{ID: "snapshot-4", AccountID: "account-4", UserID: userID, CurrentBalance: &loanBalance},
	}

	// Mock accounts with different types
	accounts := []domain.Account{
		{ID: "account-1", UserID: userID, Name: "Checking", Type: "depository"},
		{ID: "account-2", UserID: userID, Name: "Brokerage", Type: "investment"},
		{ID: "account-3", UserID: userID, Name: "Credit Card", Type: "credit"},
		{ID: "account-4", UserID: userID, Name: "Student Loan", Type: "loan"},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)

	// Mock previous snapshots for period comparison (30 days ago)
	prevDeposit := 4500.00
	prevInvest := 9500.00
	prevCredit := -1800.00
	prevLoan := -15500.00
	previousSnapshots := []domain.BalanceSnapshot{
		{ID: "prev-1", AccountID: "account-1", CurrentBalance: &prevDeposit},
		{ID: "prev-2", AccountID: "account-2", CurrentBalance: &prevInvest},
		{ID: "prev-3", AccountID: "account-3", CurrentBalance: &prevCredit},
		{ID: "prev-4", AccountID: "account-4", CurrentBalance: &prevLoan},
	}
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previousSnapshots, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Expected: Assets = 5000 + 10000 = 15000
	// Expected: Liabilities = abs(-2000) + abs(-15000) = 17000
	// Expected: Net Worth = 15000 - 17000 = -2000
	assert.Equal(t, 15000.00, result.TotalAssets)
	assert.Equal(t, 17000.00, result.TotalLiabilities)
	assert.Equal(t, -2000.00, result.NetWorth)

	// Check category breakdown
	assert.Contains(t, result.ByCategory, "assets")
	assert.Contains(t, result.ByCategory, "liabilities")
	assert.Equal(t, 15000.00, result.ByCategory["assets"].Total)
	assert.Equal(t, 2, result.ByCategory["assets"].AccountCount)
	assert.Equal(t, 17000.00, result.ByCategory["liabilities"].Total)
	assert.Equal(t, 2, result.ByCategory["liabilities"].AccountCount)

	// Check period comparison
	// Previous: Assets = 4500 + 9500 = 14000, Liabilities = 1800 + 15500 = 17300
	// Previous Net Worth = 14000 - 17300 = -3300
	// Change = -2000 - (-3300) = 1300 (improved!)
	assert.NotNil(t, result.PeriodComparison)
	assert.Equal(t, -3300.00, result.PeriodComparison.PreviousNetWorth)
	assert.Equal(t, 1300.00, result.PeriodComparison.NetWorthChange)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_ZeroAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Setup mocks - empty
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return([]domain.BalanceSnapshot{}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{}, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0.00, result.NetWorth)
	assert.Equal(t, 0.00, result.TotalAssets)
	assert.Equal(t, 0.00, result.TotalLiabilities)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_OnlyAssets(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Only asset accounts
	checkingBalance := 3000.00
	savingsBalance := 7000.00

	snapshots := []domain.BalanceSnapshot{
		{ID: "snapshot-1", AccountID: "account-1", UserID: userID, CurrentBalance: &checkingBalance},
		{ID: "snapshot-2", AccountID: "account-2", UserID: userID, CurrentBalance: &savingsBalance},
	}

	accounts := []domain.Account{
		{ID: "account-1", UserID: userID, Name: "Checking", Type: "depository"},
		{ID: "account-2", UserID: userID, Name: "Savings", Type: "depository"},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 10000.00, result.NetWorth)        // 3000 + 7000
	assert.Equal(t, 10000.00, result.TotalAssets)     // All assets
	assert.Equal(t, 0.00, result.TotalLiabilities)    // No liabilities
	assert.Contains(t, result.ByCategory, "assets")
	assert.NotContains(t, result.ByCategory, "liabilities") // No liabilities category

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_OnlyLiabilities(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Only liability accounts (negative net worth scenario)
	creditBalance := -5000.00
	loanBalance := -20000.00

	snapshots := []domain.BalanceSnapshot{
		{ID: "snapshot-1", AccountID: "account-1", UserID: userID, CurrentBalance: &creditBalance},
		{ID: "snapshot-2", AccountID: "account-2", UserID: userID, CurrentBalance: &loanBalance},
	}

	accounts := []domain.Account{
		{ID: "account-1", UserID: userID, Name: "Credit Card", Type: "credit"},
		{ID: "account-2", UserID: userID, Name: "Mortgage", Type: "loan"},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, -25000.00, result.NetWorth)         // 0 - 25000 = -25000
	assert.Equal(t, 0.00, result.TotalAssets)           // No assets
	assert.Equal(t, 25000.00, result.TotalLiabilities)  // 5000 + 20000
	assert.NotContains(t, result.ByCategory, "assets")  // No assets category
	assert.Contains(t, result.ByCategory, "liabilities")

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_OtherAccountTypeDefaultsToAsset(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// "other" account type should be classified as asset
	otherBalance := 1000.00

	snapshots := []domain.BalanceSnapshot{
		{ID: "snapshot-1", AccountID: "account-1", UserID: userID, CurrentBalance: &otherBalance},
	}

	accounts := []domain.Account{
		{ID: "account-1", UserID: userID, Name: "Other Account", Type: "other"},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1000.00, result.NetWorth)
	assert.Equal(t, 1000.00, result.TotalAssets) // "other" counts as asset
	assert.Equal(t, 0.00, result.TotalLiabilities)
	assert.Contains(t, result.ByCategory, "assets")
	assert.Contains(t, result.ByCategory["assets"].ByType, "other")

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_SnapshotRepoError(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(nil, assert.AnError)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get balance snapshots")

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetNetWorthHistory_Success(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Mock history points with type breakdown
	history := []domain.BalanceHistoryPoint{
		{
			Date:         startDate,
			TotalBalance: 10000.00,
			ByType: map[string]float64{
				"depository": 5000.00,
				"investment": 8000.00,
				"credit":     -2000.00,
				"loan":       -1000.00,
			},
		},
		{
			Date:         startDate.AddDate(0, 0, 3),
			TotalBalance: 11000.00,
			ByType: map[string]float64{
				"depository": 5500.00,
				"investment": 8500.00,
				"credit":     -2000.00,
				"loan":       -1000.00,
			},
		},
	}

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}
	result, err := service.GetNetWorthHistory(ctx, userID, filters)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, startDate, result.StartDate)
	assert.Equal(t, endDate, result.EndDate)
	assert.Equal(t, "daily", result.Granularity)
	assert.Len(t, result.Snapshots, 2)

	// First snapshot: Assets = 5000 + 8000 = 13000, Liabilities = 2000 + 1000 = 3000, Net Worth = 10000
	assert.Equal(t, 10000.00, result.Snapshots[0].NetWorth)
	assert.Equal(t, 13000.00, result.Snapshots[0].TotalAssets)
	assert.Equal(t, 3000.00, result.Snapshots[0].TotalLiabilities)

	// Second snapshot: Assets = 5500 + 8500 = 14000, Liabilities = 3000, Net Worth = 11000
	assert.Equal(t, 11000.00, result.Snapshots[1].NetWorth)
	assert.Equal(t, 14000.00, result.Snapshots[1].TotalAssets)
	assert.Equal(t, 3000.00, result.Snapshots[1].TotalLiabilities)

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetNetWorthHistory_EmptyHistory(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Setup mocks - empty history
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return([]domain.BalanceHistoryPoint{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}
	result, err := service.GetNetWorthHistory(ctx, userID, filters)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Snapshots, 0) // Empty but not nil

	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetNetWorthHistory_InvalidDateRange(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC()
	endDate := startDate.AddDate(0, 0, -7) // End before start

	// Setup mocks
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	filters := &domain.DashboardFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}
	result, err := service.GetNetWorthHistory(ctx, userID, filters)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "start_date must be before or equal to end_date")
}

func TestAccountTypeCategory(t *testing.T) {
	// Test the helper function
	tests := []struct {
		accountType string
		expected    string
	}{
		{"depository", "assets"},
		{"investment", "assets"},
		{"credit", "liabilities"},
		{"loan", "liabilities"},
		{"other", "assets"}, // Defaults to assets
	}

	for _, tc := range tests {
		t.Run(tc.accountType, func(t *testing.T) {
			result := domain.AccountTypeCategory(tc.accountType)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestGetBalanceSummary_ExcludesReplacedAccounts verifies that replaced accounts
// are excluded from balance summary (Issue #1109: re-link chain transparency).
// The exclusion happens at the repository layer (GetByUserID now returns status='active' only),
// so snapshots for replaced account IDs will not match any account in the map.
func TestGetBalanceSummary_ExcludesReplacedAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	activeBalance := 2000.00
	replacedBalance := 1500.00

	// Snapshots for both active and replaced account
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snapshot-active",
			AccountID:      "account-active",
			UserID:         userID,
			CurrentBalance: &activeBalance,
			SnapshotAt:     time.Now().UTC(),
		},
		{
			ID:             "snapshot-replaced",
			AccountID:      "account-replaced",
			UserID:         userID,
			CurrentBalance: &replacedBalance,
			SnapshotAt:     time.Now().UTC(),
		},
	}

	// GetByUserID now returns only active accounts (replaced filtered by repo)
	activeOnlyAccounts := []domain.Account{
		{
			ID:     "account-active",
			UserID: userID,
			Name:   "Active Checking",
			Type:   "depository",
			Status: domain.AccountStatusActive,
		},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeOnlyAccounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]domain.BalanceSnapshot{}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	// Only the active account's balance should count (replaced snapshot has no matching account)
	assert.Equal(t, 2000.00, result.TotalBalance)
	assert.Equal(t, 1, result.ByType["depository"].AccountCount)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetAccountTypes_ExcludesReplacedAccounts verifies that replaced accounts
// are excluded from account type listing (Issue #1109).
func TestGetAccountTypes_ExcludesReplacedAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	activeBalance := 3000.00
	replacedBalance := 1000.00

	// Snapshots for both active and replaced account
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snap-active",
			AccountID:      "account-active",
			UserID:         userID,
			CurrentBalance: &activeBalance,
			SnapshotAt:     time.Now().UTC(),
		},
		{
			ID:             "snap-replaced",
			AccountID:      "account-replaced",
			UserID:         userID,
			CurrentBalance: &replacedBalance,
			SnapshotAt:     time.Now().UTC(),
		},
	}

	// GetByUserID returns only active — replaced is excluded at repo level
	activeOnlyAccounts := []domain.Account{
		{
			ID:     "account-active",
			UserID: userID,
			Name:   "Active Checking",
			Type:   "depository",
			Status: domain.AccountStatusActive,
		},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeOnlyAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetAccountTypes(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	// Only one account type (depository) with 1 account — replaced account excluded
	assert.Len(t, result.Types, 1)
	assert.Equal(t, "depository", result.Types[0].Key)
	assert.Equal(t, 1, result.Types[0].Count)
	assert.Equal(t, 3000.00, result.Types[0].TotalBalance)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestDashboardService_GetNetWorth_ExcludesReplacedAccounts(t *testing.T) {
	// Two accounts share the same user_account_id (same logical account after re-link).
	// One is 'replaced' (old snapshot $500) and one is 'active' (snapshot $800).
	// GetNetWorth must sum only the active account's balance ($800), NOT double-count ($1300).
	ctx := context.Background()
	userID := "test-user-net-worth"
	stableUserAccountID := "user-acct-stable-001"

	activeBalance := 800.00
	// replacedBalance is intentionally NOT in the active accountMap so it's excluded

	// GetLatestByUserID returns snapshots for both accounts
	snapshots := []domain.BalanceSnapshot{
		{
			ID:             "snap-active",
			AccountID:      "account-active",
			UserID:         userID,
			CurrentBalance: &activeBalance,
			SnapshotAt:     time.Now().UTC(),
		},
	}

	// GetByUserID returns only the active account (GetByUserID is active-only)
	activeAccount := domain.Account{
		ID:            "account-active",
		UserID:        userID,
		Name:          "Savings (current)",
		Type:          "depository",
		Status:        domain.AccountStatusActive,
		UserAccountID: &stableUserAccountID,
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	// Period comparison
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return([]domain.BalanceSnapshot{}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)

	assert.NoError(t, err)
	require.NotNil(t, result)

	// Should be $800 (active only), NOT $1300 (double-count of replaced + active)
	assert.Equal(t, 800.00, result.TotalAssets,
		"GetNetWorth must not double-count replaced + active account for same user_account_id")
	assert.Equal(t, 0.00, result.TotalLiabilities)
	assert.Equal(t, 800.00, result.NetWorth)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}
