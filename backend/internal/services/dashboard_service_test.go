package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// defaultCurrentCutoff and defaultBaselineCutoff are the EXACT asOf cutoffs the personal dashboard
// paths must request when no as_of_date filter is supplied: end of today, and end of the day 30 days
// before it. Stubs below match on these values rather than mock.AnythingOfType("time.Time") on
// purpose — #1537 T69 exists because the old stubs accepted any bound, so a zero-width
// [midnight, midnight] window that matches nothing in Postgres looked like a working read for years.
// An expectation on an exact cutoff makes a wrong cutoff an unmatched call, which panics, which
// fails.
func defaultCurrentCutoff() time.Time { return endOfDay(time.Now().UTC()) }

func defaultBaselineCutoff() time.Time { return endOfDay(time.Now().UTC().AddDate(0, 0, -30)) }

// MockBalanceSnapshotRepository is a mock for BalanceSnapshotRepository
type MockBalanceSnapshotRepository struct {
	mock.Mock
}

func (m *MockBalanceSnapshotRepository) Create(ctx context.Context, snapshot *domain.BalanceSnapshot) error {
	args := m.Called(ctx, snapshot)
	return args.Error(0)
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

func (m *MockBalanceSnapshotRepository) GetSnapshotsAsOfForUser(ctx context.Context, userID string, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	args := m.Called(ctx, userID, asOf)
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

// #1569: the per-account retained balance history is a HISTORICAL read served by its own service.
// The dashboard answers current-state and aggregate questions and must never reach it, so this
// panics rather than returning a zero value a mistaken caller could silently consume.
func (m *MockBalanceSnapshotRepository) GetAccountBalanceHistory(context.Context, string, string, *time.Time, *time.Time) ([]domain.AccountBalanceHistoryPoint, error) {
	panic("the dashboard must not read per-account retained balance history")
}

// #1626 Phase 3, Defect 3: the periodic gap-filler's two methods. Real testify stubs rather than
// panics — unlike the historical read above, these ARE exercised from this package's tests
// (balance_capture_service_test.go), and a panic would make that whole suite unwritable.
func (m *MockBalanceSnapshotRepository) CreateDailyCapture(ctx context.Context, snapshot *domain.BalanceSnapshot) (bool, error) {
	args := m.Called(ctx, snapshot)
	return args.Bool(0), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) AccountsMissingDailySnapshot(ctx context.Context, linkedItemID string, day time.Time) ([]string, error) {
	args := m.Called(ctx, linkedItemID, day)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) GetSnapshotsAsOfForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	args := m.Called(ctx, requestingUserID, scope, asOf)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceSnapshot), args.Error(1)
}

func (m *MockBalanceSnapshotRepository) GetAggregatedHistoryForHousehold(ctx context.Context, requestingUserID string, scope *domain.HouseholdScope, startDate, endDate time.Time, accountTypes []string) ([]domain.BalanceHistoryPoint, error) {
	args := m.Called(ctx, requestingUserID, scope, startDate, endDate, accountTypes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceHistoryPoint), args.Error(1)
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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previousSnapshots, nil)

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
	assert.Equal(t, 3800.00, result.PeriodComparison.PreviousBalance)    // 1400 + 2400
	assert.Equal(t, 200.00, result.PeriodComparison.ChangeAmount)        // 4000 - 3800
	assert.InDelta(t, 5.26, result.PeriodComparison.ChangePercent, 0.01) // (200/3800)*100

	// #1631 Batch F1: AsOfDate is DATE-shaped, so AsOfDateDisplay (the field that actually
	// serializes as as_of_date) must be populated at this construction site.
	assert.Equal(t, result.AsOfDate.Format(time.DateOnly), result.AsOfDateDisplay)
	assert.NotEmpty(t, result.AsOfDateDisplay)

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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(nil, assert.AnError)

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

	// #1631 Batch F1: StartDate/EndDate/Date are DATE-shaped, so the Display siblings that
	// actually serialize on the wire must be populated at this construction site (response and
	// per-snapshot loop both).
	assert.Equal(t, startDate.Format(time.DateOnly), result.StartDateDisplay)
	assert.Equal(t, endDate.Format(time.DateOnly), result.EndDateDisplay)
	assert.Equal(t, startDate.Format(time.DateOnly), result.Snapshots[0].DateDisplay)
	assert.Equal(t, startDate.AddDate(0, 0, 3).Format(time.DateOnly), result.Snapshots[1].DateDisplay)

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
		switch result.Types[i].Key {
		case "depository":
			depositoryType = &result.Types[i]
		case "credit":
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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previousSnapshots, nil)

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

	// #1631 Batch F1: AsOfDate is DATE-shaped, so AsOfDateDisplay (the field that actually
	// serializes as as_of_date) must be populated at this construction site.
	assert.Equal(t, result.AsOfDate.Format(time.DateOnly), result.AsOfDateDisplay)
	assert.NotEmpty(t, result.AsOfDateDisplay)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_ZeroAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	// Setup mocks - empty
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return([]domain.BalanceSnapshot{}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 10000.00, result.NetWorth)     // 3000 + 7000
	assert.Equal(t, 10000.00, result.TotalAssets)  // All assets
	assert.Equal(t, 0.00, result.TotalLiabilities) // No liabilities
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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

	// Create service
	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	// Execute
	result, err := service.GetNetWorth(ctx, userID, nil)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, -25000.00, result.NetWorth)        // 0 - 25000 = -25000
	assert.Equal(t, 0.00, result.TotalAssets)          // No assets
	assert.Equal(t, 25000.00, result.TotalLiabilities) // 5000 + 20000
	assert.NotContains(t, result.ByCategory, "assets") // No assets category
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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(nil, assert.AnError)

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

	// #1631 Batch F1: StartDate/EndDate/Date are DATE-shaped, so the Display siblings that
	// actually serialize on the wire must be populated at this construction site (response and
	// per-snapshot loop both).
	assert.Equal(t, startDate.Format(time.DateOnly), result.StartDateDisplay)
	assert.Equal(t, endDate.Format(time.DateOnly), result.EndDateDisplay)
	assert.Equal(t, startDate.Format(time.DateOnly), result.Snapshots[0].DateDisplay)
	assert.Equal(t, startDate.AddDate(0, 0, 3).Format(time.DateOnly), result.Snapshots[1].DateDisplay)

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

// TestGetNetWorthHistory_LiabilitiesSignedCorrectly ensures the service correctly handles
// negative-balance liability accounts: TotalLiabilities must be positive after abs-normalization,
// and NetWorth = TotalAssets - TotalLiabilities. Regression for Issue #1173 Bug A sign-flip.
func TestGetNetWorthHistory_LiabilitiesSignedCorrectly(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	// Plaid typically returns credit/loan balances as negative values in our snapshot store.
	history := []domain.BalanceHistoryPoint{
		{
			Date:         startDate,
			TotalBalance: 100000.00,
			ByType: map[string]float64{
				"depository": 20000.00,
				"investment": 80000.00,
				"credit":     -2000.00,  // negative → abs-normalization should make this 2000 liability
				"loan":       -56302.00, // negative → abs-normalization should make this 56302 liability
			},
		},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	filters := &domain.DashboardFilters{StartDate: &startDate, EndDate: &endDate}
	result, err := service.GetNetWorthHistory(ctx, userID, filters)

	assert.NoError(t, err)
	require.Len(t, result.Snapshots, 1)
	snap := result.Snapshots[0]

	// Assets: depository + investment
	assert.InDelta(t, 100000.00, snap.TotalAssets, 0.01)
	// Liabilities must be positive (abs of credit + abs of loan)
	assert.InDelta(t, 58302.00, snap.TotalLiabilities, 0.01, "TotalLiabilities must be positive after abs-normalization")
	// NetWorth = assets - liabilities
	assert.InDelta(t, 41698.00, snap.NetWorth, 0.01)

	mockSnapshotRepo.AssertExpectations(t)
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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeOnlyAccounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return([]domain.BalanceSnapshot{}, nil)

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

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(snapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	// Period comparison
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
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

// TestDashboardService_GetNetWorth_ResolvesByUserAccountID verifies that a snapshot
// whose account_id points at a replaced row is still included via the user_account_id
// chain key.
func TestDashboardService_GetNetWorth_ResolvesByUserAccountID(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-chain-resolve"
	stableUserAccountID := "ua-chain-stable-xyz"

	activeBalance := 600.00
	snapshot := domain.BalanceSnapshot{
		ID:             "snap-from-replaced",
		AccountID:      "account-replaced-id",
		UserAccountID:  &stableUserAccountID,
		UserID:         userID,
		CurrentBalance: &activeBalance,
		SnapshotAt:     time.Now().UTC(),
	}

	activeAccount := domain.Account{
		ID:            "account-active-id",
		UserID:        userID,
		Name:          "Checking (active)",
		Type:          "depository",
		Status:        domain.AccountStatusActive,
		UserAccountID: &stableUserAccountID,
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return([]domain.BalanceSnapshot{snapshot}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return([]domain.BalanceSnapshot{}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 600.00, result.TotalAssets,
		"GetNetWorth must resolve snapshot via user_account_id when snapshot.AccountID is a replaced row")
	assert.Equal(t, 600.00, result.NetWorth)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestDashboardService_GetNetWorth_PreviousPeriodResolvesChain verifies that the
// previous-period snapshot loop also resolves via user_account_id. Without the fix
// a historical snapshot tagged with a replaced account_id would be silently dropped,
// making period-over-period delta calculations wrong after a re-link.
func TestDashboardService_GetNetWorth_PreviousPeriodResolvesChain(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-prev-period-chain"
	stableUA := "ua-prev-period-stable"

	currentBalance := 700.00
	previousBalance := 500.00

	currentSnapshot := domain.BalanceSnapshot{
		ID:             "snap-current",
		AccountID:      "account-active-id",
		UserAccountID:  &stableUA,
		UserID:         userID,
		CurrentBalance: &currentBalance,
		SnapshotAt:     time.Now().UTC(),
	}
	// Previous snapshot carries the OLD replaced account_id but the stable user_account_id.
	previousSnapshot := domain.BalanceSnapshot{
		ID:             "snap-previous-replaced",
		AccountID:      "account-replaced-id",
		UserAccountID:  &stableUA,
		UserID:         userID,
		CurrentBalance: &previousBalance,
		SnapshotAt:     time.Now().UTC().AddDate(0, 0, -30),
	}

	activeAccount := domain.Account{
		ID:            "account-active-id",
		UserID:        userID,
		Name:          "Checking",
		Type:          "depository",
		Status:        domain.AccountStatusActive,
		UserAccountID: &stableUA,
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return([]domain.BalanceSnapshot{currentSnapshot}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return([]domain.BalanceSnapshot{previousSnapshot}, nil)
	// #1569 T10-FIX item 2: the baseline universe is now resolved whenever a snapshot's OWN
	// accounts row is missing from the current map — which is exactly this snapshot's situation —
	// rather than only when its chain key misses too. The predecessor was live at the cutoff.
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).
		Return([]domain.Account{activeAccount, {
			ID: "account-replaced-id", UserID: userID, Name: "Checking (old)", Type: "depository",
			Status: domain.AccountStatusReplaced, UserAccountID: &stableUA,
		}}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 700.00, result.TotalAssets, "current period should reflect active snapshot")
	require.NotNil(t, result.PeriodComparison,
		"previous-period snapshot must be resolved via user_account_id, not dropped")
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousAssets,
		"pre-relink snapshot from replaced account must be included in previous-period total")
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.01,
		"period delta must reflect diff between current and pre-relink snapshot")

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetBalanceSummary_PreviousPeriodChainDedup verifies that when both a replaced and
// an active chain member have a previous-period snapshot, the balance is counted only
// once (not double-counted). Issue #1109 W4-B.
func TestGetBalanceSummary_PreviousPeriodChainDedup(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-dedup"
	stableUA := "ua-dedup-stable"

	currentBalance := 700.00
	prevBalanceReplaced := 500.00 // snapshot tagged with replaced account
	prevBalanceActive := 500.00   // snapshot tagged with active account — same value, same chain

	currentSnapshot := domain.BalanceSnapshot{
		ID:             "snap-current",
		AccountID:      "account-active",
		UserAccountID:  &stableUA,
		UserID:         userID,
		CurrentBalance: &currentBalance,
	}

	// Two previous-period snapshots for the SAME logical account (one per chain member).
	// If both are summed the result is 1000; correct dedup gives 500.
	prevSnapReplaced := domain.BalanceSnapshot{
		ID:             "prev-replaced",
		AccountID:      "account-replaced",
		UserAccountID:  &stableUA,
		CurrentBalance: &prevBalanceReplaced,
	}
	prevSnapActive := domain.BalanceSnapshot{
		ID:             "prev-active",
		AccountID:      "account-active",
		UserAccountID:  &stableUA,
		CurrentBalance: &prevBalanceActive,
	}

	activeAccount := domain.Account{
		ID:            "account-active",
		UserID:        userID,
		Name:          "Checking",
		Type:          "depository",
		Status:        domain.AccountStatusActive,
		UserAccountID: &stableUA,
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return([]domain.BalanceSnapshot{currentSnapshot}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return([]domain.BalanceSnapshot{prevSnapReplaced, prevSnapActive}, nil)
	// #1569 T10-FIX item 2: the baseline universe now resolves whenever a snapshot's OWN accounts
	// row is missing. Supplying the predecessor as 'replaced' is what lets the collapse below
	// happen for the RIGHT reason — a recorded predecessor deferring to its live successor.
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).
		Return([]domain.Account{activeAccount, {
			ID: "account-replaced", UserID: userID, Name: "Checking (old)", Type: "depository",
			Status: domain.AccountStatusReplaced, UserAccountID: &stableUA,
		}}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)

	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousBalance,
		"both replaced and active snapshots share the same chain key — must not be double-counted")
	assert.Equal(t, 200.00, result.PeriodComparison.ChangeAmount)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetNetWorth_PreviousPeriodChainDedup verifies that when both a replaced and an
// active chain member have a previous-period snapshot, net worth period comparison
// counts that chain only once. Issue #1109 W4-B.
func TestGetNetWorth_PreviousPeriodChainDedup(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-dedup"
	stableUA := "ua-nw-dedup-stable"

	currentBalance := 700.00
	prevBalance := 500.00

	currentSnapshot := domain.BalanceSnapshot{
		ID:             "snap-current",
		AccountID:      "account-active",
		UserAccountID:  &stableUA,
		UserID:         userID,
		CurrentBalance: &currentBalance,
	}

	prevSnapReplaced := domain.BalanceSnapshot{
		ID:             "prev-replaced",
		AccountID:      "account-replaced",
		UserAccountID:  &stableUA,
		CurrentBalance: &prevBalance,
	}
	prevSnapActive := domain.BalanceSnapshot{
		ID:             "prev-active",
		AccountID:      "account-active",
		UserAccountID:  &stableUA,
		CurrentBalance: &prevBalance,
	}

	activeAccount := domain.Account{
		ID:            "account-active",
		UserID:        userID,
		Name:          "Checking",
		Type:          "depository",
		Status:        domain.AccountStatusActive,
		UserAccountID: &stableUA,
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return([]domain.BalanceSnapshot{currentSnapshot}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{activeAccount}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return([]domain.BalanceSnapshot{prevSnapReplaced, prevSnapActive}, nil)
	// #1569 T10-FIX item 2: see the balance-summary twin — the predecessor must be in the baseline
	// universe for the collapse to happen for the right reason.
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).
		Return([]domain.Account{activeAccount, {
			ID: "account-replaced", UserID: userID, Name: "Checking (old)", Type: "depository",
			Status: domain.AccountStatusReplaced, UserAccountID: &stableUA,
		}}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)

	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousAssets,
		"both replaced and active snapshots share the same chain key — must not be double-counted")
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.01)

	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// ─── #1369 — Phase 5: household balance/net-worth aggregation ───────────────
//
// Household mode must use GetSnapshotsAsOfForHousehold / GetForHouseholdScopeActiveNow — NOT
// GetLatestByUserID / GetByUserID (and GetByUserIDAndDateRange, deleted in #1537) — single-user, and
// would silently drop every non-requester member's data (Codex adversarial review,
// P0-2/P0-3). These are pure-Go aggregation tests (repo correctness is covered by
// integration tests); they assert the SERVICE picks the right repo methods and sums
// correctly across household accounts.

func TestGetBalanceSummary_Household_AggregatesAcrossMembers(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	ownerBalance := 1000.00
	memberBalance := 500.00
	snapshots := []domain.BalanceSnapshot{
		{ID: "s1", AccountID: "acct-owner", UserID: "owner-1", CurrentBalance: &ownerBalance, SnapshotAt: time.Now().UTC()},
		{ID: "s2", AccountID: "acct-member", UserID: "member-2", CurrentBalance: &memberBalance, SnapshotAt: time.Now().UTC()},
	}
	accounts := []domain.Account{
		{ID: "acct-owner", UserID: "owner-1", Name: "Owner Checking", Type: "depository"},
		{ID: "acct-member", UserID: "member-2", Name: "Member Checking", Type: "depository"},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.AnythingOfType("time.Time")).Return(snapshots, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, mock.AnythingOfType("time.Time")).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	filters := &domain.DashboardFilters{HouseholdScope: scope}
	result, err := service.GetBalanceSummary(ctx, requestingUserID, filters)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1500.00, result.TotalBalance, "owner's $1000 + member's $500 must both be counted")
	assert.Equal(t, 2, result.ByType["depository"].AccountCount)

	// Must NOT call the single-user methods — they would silently drop member-2's data.
	mockSnapshotRepo.AssertNotCalled(t, "GetLatestByUserID", mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "GetByUserID", mock.Anything, mock.Anything)
	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestGetNetWorth_Household_AggregatesAcrossMembers(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	ownerBalance := 1000.00
	memberBalance := 500.00
	snapshots := []domain.BalanceSnapshot{
		{ID: "s1", AccountID: "acct-owner", UserID: "owner-1", CurrentBalance: &ownerBalance, SnapshotAt: time.Now().UTC()},
		{ID: "s2", AccountID: "acct-member", UserID: "member-2", CurrentBalance: &memberBalance, SnapshotAt: time.Now().UTC()},
	}
	accounts := []domain.Account{
		{ID: "acct-owner", UserID: "owner-1", Name: "Owner Checking", Type: "depository"},
		{ID: "acct-member", UserID: "member-2", Name: "Member Checking", Type: "depository"},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.AnythingOfType("time.Time")).Return(snapshots, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, mock.AnythingOfType("time.Time")).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	filters := &domain.DashboardFilters{HouseholdScope: scope}
	result, err := service.GetNetWorth(ctx, requestingUserID, filters)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1500.00, result.TotalAssets, "owner's $1000 + member's $500 must both be counted")
	assert.Equal(t, 1500.00, result.NetWorth)

	mockSnapshotRepo.AssertNotCalled(t, "GetLatestByUserID", mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "GetByUserID", mock.Anything, mock.Anything)
}

// TestGetNetWorth_Household_PreviousPeriod_JointAccountResolvesViaMatchingAsOf guards
// against gap-closure G1: the previous-period accountMap must be built with the SAME asOf
// cutoff as the previous-period snapshot query, or a joint account whose globally-freshest
// copy has no pre-cutoff history gets silently dropped from the previous-period comparison.
func TestGetNetWorth_Household_PreviousPeriod_JointAccountResolvesViaMatchingAsOf(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	currentBalance := 1000.0
	currentSnapshots := []domain.BalanceSnapshot{
		{ID: "s-current", AccountID: "acct-memberB", UserAccountID: strPtr("ua-memberB"), CurrentBalance: &currentBalance, SnapshotAt: time.Now().UTC()},
	}
	currentAccounts := []domain.Account{
		{ID: "acct-memberB", UserAccountID: strPtr("ua-memberB"), Name: "Joint (member B copy)", Type: "depository"},
	}

	// Previous-period: the asOf-aware snapshot query resolves to member A's copy (the
	// globally-freshest member B copy has no snapshot before the cutoff), but the OLD
	// single-accountMap design would only know about member B's account.
	previousBalance := 800.0
	previousSnapshots := []domain.BalanceSnapshot{
		{ID: "s-prev", AccountID: "acct-memberA", UserAccountID: strPtr("ua-memberA"), CurrentBalance: &previousBalance, SnapshotAt: time.Now().UTC().AddDate(0, 0, -35)},
	}
	previousAccounts := []domain.Account{
		{ID: "acct-memberA", UserAccountID: strPtr("ua-memberA"), Name: "Joint (member A copy)", Type: "depository"},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(func(tm time.Time) bool {
		return time.Since(tm) < time.Minute // "now"-ish call
	})).Return(currentSnapshots, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, mock.MatchedBy(func(tm time.Time) bool {
		return time.Since(tm) < time.Minute
	})).Return(currentAccounts, nil)

	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(func(tm time.Time) bool {
		d := time.Since(tm)
		return d > 29*24*time.Hour && d < 31*24*time.Hour // ~30-days-ago call
	})).Return(previousSnapshots, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, mock.MatchedBy(func(tm time.Time) bool {
		d := time.Since(tm)
		return d > 29*24*time.Hour && d < 31*24*time.Hour
	})).Return(previousAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	filters := &domain.DashboardFilters{HouseholdScope: scope}
	result, err := service.GetNetWorth(ctx, requestingUserID, filters)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison, "previous-period comparison must not be silently skipped")
	assert.Equal(t, 800.0, result.PeriodComparison.PreviousAssets,
		"previous period must resolve member A's joint-account copy via a matching-asOf accountMap, not silently drop it")
}

func TestGetBalanceHistory_Household_UsesHouseholdAggregatedHistory(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	history := []domain.BalanceHistoryPoint{
		{Date: startDate, TotalBalance: 1500.00, ByType: map[string]float64{"depository": 1500.00}},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetAggregatedHistoryForHousehold", ctx, requestingUserID, scope,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	filters := &domain.DashboardFilters{StartDate: &startDate, EndDate: &endDate, HouseholdScope: scope}
	result, err := service.GetBalanceHistory(ctx, requestingUserID, filters)

	require.NoError(t, err)
	require.Len(t, result.Snapshots, 1)
	assert.Equal(t, 1500.00, result.Snapshots[0].Balance)

	mockSnapshotRepo.AssertNotCalled(t, "GetAggregatedHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockSnapshotRepo.AssertExpectations(t)
}

func TestGetNetWorthHistory_Household_UsesHouseholdAggregatedHistory(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}
	startDate := time.Now().UTC().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	history := []domain.BalanceHistoryPoint{
		{Date: startDate, TotalBalance: 1500.00, ByType: map[string]float64{"depository": 1500.00}},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetAggregatedHistoryForHousehold", ctx, requestingUserID, scope,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	filters := &domain.DashboardFilters{StartDate: &startDate, EndDate: &endDate, HouseholdScope: scope}
	result, err := service.GetNetWorthHistory(ctx, requestingUserID, filters)

	require.NoError(t, err)
	require.Len(t, result.Snapshots, 1)
	assert.Equal(t, 1500.00, result.Snapshots[0].TotalAssets)

	mockSnapshotRepo.AssertNotCalled(t, "GetAggregatedHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestGetNetWorthHistory_UntypedBalance_IsNotReadAsZeroNetWorth is #1626 Phase 3 item 5.
//
// THE DEFECT. Net-worth history is reconstructed EXCLUSIVELY from a point's ByType breakdown, and
// an ORPHANED balance snapshot — accounts row hard-deleted, balance_snapshots.account_id SET NULL —
// carries no type. GetAggregatedHistory counted it toward TotalBalance and omitted it from ByType,
// so a day whose only rows were such orphans reported net worth of EXACTLY ZERO next to a correct
// balance figure: the chart's "your net worth collapsed" day.
//
// The repository now recovers a type from the snapshot's own chain row (user_accounts.identity_type)
// wherever one survives, which is both the common case and the only way to get the SIGN right for a
// credit card. This test covers the residue that recovery cannot reach — a snapshot whose chain row
// is gone too — and it is a SERVICE-level property because the service is where ByType becomes a
// net worth.
func TestGetNetWorthHistory_UntypedBalance_IsNotReadAsZeroNetWorth(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -2).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	history := []domain.BalanceHistoryPoint{
		// A day of fully-detached orphans: a real total, no type breakdown at all.
		{Date: startDate, TotalBalance: 4200.00, ByType: map[string]float64{}, UntypedBalance: 4200.00},
		// A mixed day: one typed account plus an untyped remainder.
		{Date: endDate, TotalBalance: 4200.00, ByType: map[string]float64{"depository": 1000.00},
			UntypedBalance: 3200.00},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"),
		mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetNetWorthHistory(ctx, userID, &domain.DashboardFilters{
		StartDate: &startDate, EndDate: &endDate,
	})
	require.NoError(t, err)
	require.Len(t, result.Snapshots, 2)

	assert.InDelta(t, 4200.00, result.Snapshots[0].NetWorth, 0.01,
		"an orphan-only day must not report net worth of exactly zero beside a $4200 balance")
	assert.InDelta(t, 4200.00, result.Snapshots[0].TotalAssets, 0.01,
		"a positive untyped balance reads as an asset at face value")

	assert.InDelta(t, 4200.00, result.Snapshots[1].NetWorth, 0.01,
		"the untyped remainder of a mixed day counts too, not just a wholly untyped day")
}

// TestGetNetWorthHistory_UntypedNegativeBalance_ReadsAsALiability is the other half: the sign of an
// untyped balance is the only signal left about what it is, so a negative one must not be added to
// assets. Without this, an untyped debt would IMPROVE the reported net worth.
func TestGetNetWorthHistory_UntypedNegativeBalance_ReadsAsALiability(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	startDate := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	endDate := time.Now().UTC().Truncate(24 * time.Hour)

	history := []domain.BalanceHistoryPoint{
		{Date: endDate, TotalBalance: -300.00, ByType: map[string]float64{}, UntypedBalance: -300.00},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetAggregatedHistory", ctx, userID, mock.AnythingOfType("time.Time"),
		mock.AnythingOfType("time.Time"), []string(nil)).Return(history, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetNetWorthHistory(ctx, userID, &domain.DashboardFilters{
		StartDate: &startDate, EndDate: &endDate,
	})
	require.NoError(t, err)
	require.Len(t, result.Snapshots, 1)

	assert.InDelta(t, 300.00, result.Snapshots[0].TotalLiabilities, 0.01,
		"a negative untyped balance is a debt, not an asset")
	assert.InDelta(t, 0.00, result.Snapshots[0].TotalAssets, 0.01)
	assert.InDelta(t, -300.00, result.Snapshots[0].NetWorth, 0.01)
}

// #1631 gap D1. BalanceSummaryResponse.AsOfDateDisplay documents its value as the UTC calendar
// day, but asOfDate is COMPUTED here — either from time.Now() or from a caller-supplied filter —
// so the day it reports was decided by whatever location that value carried.
//
// The instant was never wrong: Truncate(24*time.Hour) floors to a UTC-midnight-aligned instant.
// Only the LOCATION was, and the location is the sole thing that decides which day
// domain.formatDateOnly prints. Every cutoff derived from asOfDate is therefore unchanged by the
// fix; the rendered day is not.
func TestGetBalanceSummary_AsOfDateDisplayIsTheUTCCalendarDay(t *testing.T) {
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	ctx := context.Background()
	userID := "user-123"

	// Midnight on 2026-07-01 in -11:00 is 2026-07-01T11:00:00Z. Truncate(24*time.Hour) floors
	// that to the 2026-07-01T00:00:00Z instant — the right instant — but leaves it LOCATED in
	// -11:00, where the same instant reads 2026-06-30T13:00. A negative offset is the direction
	// that bites: the truncated instant is already UTC-midnight-aligned, so a positive offset
	// happens to render the UTC day anyway and hides the defect.
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("-11", -11*3600))
	filters := &domain.DashboardFilters{AsOfDate: &asOf}

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, mock.AnythingOfType("time.Time")).
		Return([]domain.BalanceSnapshot{}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, filters)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "2026-07-01", result.AsOfDateDisplay,
		"as_of_date must be the UTC calendar day, not the day the caller's offset names")
	// The two must not be allowed to drift apart: AsOfDate is the SQL-bound sibling and the
	// display is what serializes.
	assert.Equal(t, result.AsOfDateDisplay, result.AsOfDate.UTC().Format(time.DateOnly))
}
