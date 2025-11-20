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
