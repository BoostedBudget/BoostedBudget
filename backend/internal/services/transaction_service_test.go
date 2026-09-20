package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTransactionRepository is a mock implementation of TransactionRepository for testing
type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) CreateBatch(ctx context.Context, transactions []domain.Transaction) (map[string]bool, error) {
	args := m.Called(ctx, transactions)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]bool), args.Error(1)
}

func (m *MockTransactionRepository) Update(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

// UpdateFromSync — #1684: the `modified` path's writer, which carries the full category tuple.
func (m *MockTransactionRepository) UpdateFromSync(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

// PullForwardSettledPendingAnnotations — #1586.
func (m *MockTransactionRepository) PullForwardSettledPendingAnnotations(ctx context.Context, userID string, transactionIDs []string) (int64, error) {
	args := m.Called(ctx, userID, transactionIDs)
	if n, ok := args.Get(0).(int64); ok {
		return n, args.Error(1)
	}
	return 0, args.Error(1)
}

func (m *MockTransactionRepository) TombstoneByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string, deletedBy string) (domain.TransactionDeletedReason, error) {
	args := m.Called(ctx, source, externalID, userID, deletedBy)
	if r, ok := args.Get(0).(domain.TransactionDeletedReason); ok {
		return r, args.Error(1)
	}
	return "", args.Error(1)
}

func (m *MockTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, source, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, filters, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error) {
	args := m.Called(ctx, userID, filters)
	return args.Int(0), args.Error(1)
}

func (m *MockTransactionRepository) GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error) {
	args := m.Called(ctx, source, externalID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) UpdateTags(ctx context.Context, id string, userID string, tags []string, tagsSource []domain.TagSource) error {
	args := m.Called(ctx, id, userID, tags, tagsSource)
	return args.Error(0)
}

func (m *MockTransactionRepository) UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error {
	args := m.Called(ctx, id, userID, overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource)
	return args.Error(0)
}

func (m *MockTransactionRepository) SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error) {
	args := m.Called(ctx, userID, transactionIDs, reviewed)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockTransactionRepository) BulkUpdateCategory(ctx context.Context, userID string, transactionIDs []string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) (int64, error) {
	args := m.Called(ctx, userID, transactionIDs, overrideKind, pfcPrimaryOverride, pfcDetailedOverride, customCategoryOverrideID, categorySource)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockTransactionRepository) GetReviewSummary(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	return &domain.ReviewSummaryResponse{TopUnreviewedCategories: []domain.TopUnreviewedCategory{}}, nil
}

func (m *MockTransactionRepository) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// Issue #492: Income Source Matching Service methods
func (m *MockTransactionRepository) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, category, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockTransactionRepository) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, tag, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockTransactionRepository) GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, budgetCat, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockTransactionRepository) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, tags, matchLogic, startDate, endDate)
	return args.Int(0), args.Get(1).(float64), args.Error(2)
}

func (m *MockTransactionRepository) GetCategoryMatchBreakdown(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) ([]domain.CategoryPreviewBreakdownItem, int, float64, int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, tags, matchLogic, startDate, endDate)
	var children []domain.CategoryPreviewBreakdownItem
	if v := args.Get(0); v != nil {
		children = v.([]domain.CategoryPreviewBreakdownItem)
	}
	return children, args.Int(1), args.Get(2).(float64), args.Int(3), args.Get(4).(float64), args.Error(5)
}

func (m *MockTransactionRepository) GetTransactionsForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	args := m.Called(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Get(2).(float64), args.Error(3)
	}
	return args.Get(0).([]domain.Transaction), args.Int(1), args.Get(2).(float64), args.Error(3)
}

// GetByUserAccountID retrieves all transactions for a user_account.
// Issue #171 - Plaid Re-Link Handling
func (m *MockTransactionRepository) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, userAccountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID string, customCategoryID string) (int64, error) {
	args := m.Called(ctx, userID, customCategoryID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockTransactionRepository) GetByUserIDKeyset(ctx context.Context, userID string, filters domain.TransactionFilters, cursor *domain.TransactionCursor, limit int) ([]domain.Transaction, *domain.TransactionCursor, *domain.FeedTotals, error) {
	args := m.Called(ctx, userID, filters, cursor, limit)
	var txs []domain.Transaction
	if v := args.Get(0); v != nil {
		txs = v.([]domain.Transaction)
	}
	var nextCursor *domain.TransactionCursor
	if v := args.Get(1); v != nil {
		nextCursor = v.(*domain.TransactionCursor)
	}
	// Whole-set totals, position 2 (#1626 Phase 2). Every .Return() must now pass it explicitly —
	// a nil there is the legitimate "not computed on this page" value, and making it positional
	// rather than optional means a test that cares about totals has to say so.
	var totals *domain.FeedTotals
	if v := args.Get(2); v != nil {
		totals = v.(*domain.FeedTotals)
	}
	return txs, nextCursor, totals, args.Error(3)
}

func (m *MockTransactionRepository) GetByUserIDKeysetUnified(ctx context.Context, userID string, filters domain.TransactionFilters, cursor *domain.TransactionCursor, limit int) ([]*domain.ActivityItem, *domain.TransactionCursor, *domain.FeedTotals, error) {
	args := m.Called(ctx, userID, filters, cursor, limit)
	var items []*domain.ActivityItem
	if v := args.Get(0); v != nil {
		items = v.([]*domain.ActivityItem)
	}
	var nextCursor *domain.TransactionCursor
	if v := args.Get(1); v != nil {
		nextCursor = v.(*domain.TransactionCursor)
	}
	// See GetByUserIDKeyset above: totals are positional at 2.
	var totals *domain.FeedTotals
	if v := args.Get(2); v != nil {
		totals = v.(*domain.FeedTotals)
	}
	return items, nextCursor, totals, args.Error(3)
}

func (m *MockTransactionRepository) GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error) {
	args := m.Called(ctx, userID, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetInflowTransactionsForAccount(ctx context.Context, userID, accountID string, start, end time.Time) ([]*domain.SavingsInflowTransaction, error) {
	args := m.Called(ctx, userID, accountID, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsInflowTransaction), args.Error(1)
}

func (m *MockTransactionRepository) ResolveEffectiveMerchantNames(ctx context.Context, userID string, merchantIDs []string) (map[string]string, error) {
	args := m.Called(ctx, userID, merchantIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

// TestTransactionService_ListTransactions_EnforcesMaxLimit tests that the service enforces a maximum limit of 100
func TestTransactionService_ListTransactions_EnforcesMaxLimit(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Attempt to request more than 100 transactions
	_, err := service.ListTransactions(ctx, userID, filters, 150, 0)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidLimit, err)
}

// TestTransactionService_ListTransactions_EnforcesNonNegativeOffset tests that negative offsets are rejected
func TestTransactionService_ListTransactions_EnforcesNonNegativeOffset(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Attempt to use negative offset
	_, err := service.ListTransactions(ctx, userID, filters, 50, -10)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidOffset, err)
}

// TestTransactionService_ListTransactions_ValidatesDateRange tests that start date must be before end date
func TestTransactionService_ListTransactions_ValidatesDateRange(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	startDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	filters := domain.TransactionFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	// Attempt to use invalid date range (start after end)
	_, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidDateRange, err)
}

// searchPassthroughCases are the test cases shared by both SearchPassesThrough tests below.
// The sanitizer matched "UNION " (with trailing space), so these cases cover the literal
// corruption scenarios from #1307 plus an injection string that demonstrates the value
// is harmless once bound as an ILIKE parameter.
var searchPassthroughCases = []struct {
	name   string
	search string
}{
	{"payee with UNION and trailing context", "Western Union Bank"},
	{"payee with UNION and trailing space", "Western Union "},
	{"literal SQL injection string", "'; DROP TABLE transactions; --"},
}

// TestTransactionService_ListTransactions_SearchPassesThrough verifies that search values —
// including payee names that contain SQL keywords and literal injection strings — reach the
// repository verbatim. The parameterised ILIKE query is the actual injection barrier; value
// mutation is unnecessary and corrupts legitimate searches like "Western Union" (#1307).
func TestTransactionService_ListTransactions_SearchPassesThrough(t *testing.T) {
	for _, tc := range searchPassthroughCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockTransactionRepository)
			service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
			ctx := context.Background()

			userID := "user-123"
			search := tc.search
			filters := domain.TransactionFilters{Search: &search}

			var gotSearch string
			mockRepo.On("GetByUserIDWithFilters", ctx, userID, mock.Anything, 50, 0).
				Run(func(args mock.Arguments) {
					f := args.Get(2).(domain.TransactionFilters)
					if f.Search != nil {
						gotSearch = *f.Search
					}
				}).
				Return([]domain.Transaction{}, nil)
			mockRepo.On("CountByUserID", ctx, userID, mock.Anything).Return(0, nil)

			_, err := service.ListTransactions(ctx, userID, filters, 50, 0)

			require.NoError(t, err)
			assert.Equal(t, tc.search, gotSearch, "search value must reach the repository unchanged")
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestTransactionService_ListTransactionsByCursor_SearchPassesThrough verifies that the
// cursor path — the path used by the live endpoint — also forwards the search value unchanged.
func TestTransactionService_ListTransactionsByCursor_SearchPassesThrough(t *testing.T) {
	for _, tc := range searchPassthroughCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockTransactionRepository)
			service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
			ctx := context.Background()

			userID := "user-123"
			search := tc.search
			filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc", Search: &search}

			var gotSearch string
			mockRepo.On("GetByUserIDKeyset", ctx, userID, mock.Anything, (*domain.TransactionCursor)(nil), 50).
				Run(func(args mock.Arguments) {
					f := args.Get(2).(domain.TransactionFilters)
					if f.Search != nil {
						gotSearch = *f.Search
					}
				}).
				Return([]domain.Transaction{}, (*domain.TransactionCursor)(nil), nil, nil)

			_, err := service.ListTransactionsByCursor(ctx, userID, filters, nil, 50)

			require.NoError(t, err)
			assert.Equal(t, tc.search, gotSearch, "search value must reach the repository unchanged")
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestTransactionService_ListTransactions_ReturnsTransactionsArray tests successful transaction retrieval
func TestTransactionService_ListTransactions_ReturnsTransactionsArray(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock transactions
	externalID := "txn-1"
	mockTransactions := []domain.Transaction{
		{
			ID:                    "id-1",
			UserID:                userID,
			AccountID:             "account-1",
			LinkedItemID:          "item-1",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                25.50,
			Date:                  time.Now(),
			Name:                  "Grocery Store",
		},
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(1, nil)

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.Transactions))
	assert.Equal(t, "id-1", result.Transactions[0].ID)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_IncludesPaginationMetadata tests that response includes pagination metadata
func TestTransactionService_ListTransactions_IncludesPaginationMetadata(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}
	limit := 50
	offset := 0
	totalCount := 150

	// Mock 50 transactions returned
	mockTransactions := make([]domain.Transaction, 50)
	for i := 0; i < 50; i++ {
		externalID := "txn-1"
		mockTransactions[i] = domain.Transaction{
			ID:                    "id-" + string(rune(i)),
			UserID:                userID,
			ExternalTransactionID: &externalID,
		}
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, limit, offset).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(totalCount, nil)

	result, err := service.ListTransactions(ctx, userID, filters, limit, offset)

	require.NoError(t, err)
	assert.NotNil(t, result)
	require.NotNil(t, result.TotalCount, "the offset path always counts, so it always sets the pointer")
	assert.Equal(t, totalCount, *result.TotalCount)
	assert.Equal(t, limit, result.Limit)
	assert.Equal(t, offset, result.Offset)
	assert.True(t, result.HasMore) // 150 total, 50 returned, so there's more
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HasMoreFalseWhenNoMoreResults tests has_more flag when all results returned
func TestTransactionService_ListTransactions_HasMoreFalseWhenNoMoreResults(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}
	limit := 50
	offset := 100
	totalCount := 120

	// Mock 20 transactions returned (last page)
	mockTransactions := make([]domain.Transaction, 20)
	for i := 0; i < 20; i++ {
		externalID := "txn-1"
		mockTransactions[i] = domain.Transaction{
			ID:                    "id-" + string(rune(i)),
			UserID:                userID,
			ExternalTransactionID: &externalID,
		}
	}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, limit, offset).Return(mockTransactions, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(totalCount, nil)

	result, err := service.ListTransactions(ctx, userID, filters, limit, offset)

	require.NoError(t, err)
	assert.NotNil(t, result)
	require.NotNil(t, result.TotalCount, "the offset path always counts, so it always sets the pointer")
	assert.Equal(t, totalCount, *result.TotalCount)
	assert.False(t, result.HasMore) // offset=100 + returned=20 = 120, no more results
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HandlesRepositoryError tests error handling from repository
func TestTransactionService_ListTransactions_HandlesRepositoryError(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock repository returning an error
	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return(nil, repository.ErrTransactionNotFound)

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get transactions")
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_HandlesCountError tests error handling from count operation
func TestTransactionService_ListTransactions_HandlesCountError(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return([]domain.Transaction{}, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(0, errors.New("database connection error"))

	result, err := service.ListTransactions(ctx, userID, filters, 50, 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to count transactions")
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactions_DefaultsLimit tests that default limit of 50 is applied
func TestTransactionService_ListTransactions_DefaultsLimit(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Pass limit=0, should default to 50
	mockRepo.On("GetByUserIDWithFilters", ctx, userID, filters, 50, 0).Return([]domain.Transaction{}, nil)
	mockRepo.On("CountByUserID", ctx, userID, filters).Return(0, nil)

	result, err := service.ListTransactions(ctx, userID, filters, 0, 0)

	require.NoError(t, err)
	assert.Equal(t, 50, result.Limit)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_GetDistinctPlaidCategories_Success tests successful retrieval of Plaid categories
func TestTransactionService_GetDistinctPlaidCategories_Success(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	expectedCategories := []string{"Coffee Shop", "Groceries", "Restaurants"}

	mockRepo.On("GetDistinctPlaidCategories", ctx, userID).Return(expectedCategories, nil)

	categories, err := service.GetDistinctPlaidCategories(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedCategories, categories)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_GetDistinctPlaidCategories_Empty tests empty result
func TestTransactionService_GetDistinctPlaidCategories_Empty(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-no-transactions"

	// Return nil from repo (no transactions)
	mockRepo.On("GetDistinctPlaidCategories", ctx, userID).Return(nil, nil)

	categories, err := service.GetDistinctPlaidCategories(ctx, userID)

	assert.NoError(t, err)
	// Should return empty slice, not nil, for consistent JSON serialization
	assert.NotNil(t, categories)
	assert.Empty(t, categories)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_GetDistinctPlaidCategories_Error tests error handling
func TestTransactionService_GetDistinctPlaidCategories_Error(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"

	mockRepo.On("GetDistinctPlaidCategories", ctx, userID).Return(nil, errors.New("database error"))

	categories, err := service.GetDistinctPlaidCategories(ctx, userID)

	assert.Error(t, err)
	assert.Nil(t, categories)
	assert.Contains(t, err.Error(), "failed to get distinct plaid categories")
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// PFC v2 typed fields in API responses
// ============================================================================

func ptrStr(s string) *string { return &s }

func TestGetTransactionByID_ReturnsPFCFields(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	primary := "FOOD_AND_DRINK"
	detailed := "FOOD_AND_DRINK_FAST_FOOD"
	confidence := "VERY_HIGH"
	txn := &domain.Transaction{
		ID:                 "txn-1",
		UserID:             "user-1",
		PFCPrimary:         &primary,
		PFCDetailed:        &detailed,
		PFCConfidenceLevel: &confidence,
	}
	mockRepo.On("GetByID", ctx, "txn-1").Return(txn, nil)

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-1")
	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary)
	assert.Equal(t, "FOOD_AND_DRINK", *result.PFCPrimary)
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", *result.PFCDetailed)
	assert.Equal(t, "VERY_HIGH", *result.PFCConfidenceLevel)
	mockRepo.AssertExpectations(t)
}

func TestGetTransactionByID_NilPFC(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	txn := &domain.Transaction{
		ID:     "txn-1",
		UserID: "user-1",
	}
	mockRepo.On("GetByID", ctx, "txn-1").Return(txn, nil)

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-1")
	require.NoError(t, err)
	assert.Nil(t, result.PFCPrimary)
	mockRepo.AssertExpectations(t)
}

// TestGetTransactionByID_PopulatesPFCDisplay asserts that service enriches display names.
func TestGetTransactionByID_PopulatesPFCDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	primary := "FOOD_AND_DRINK"
	txn := &domain.Transaction{
		ID:         "txn-pfc",
		UserID:     "user-1",
		PFCPrimary: &primary,
	}
	mockRepo.On("GetByID", ctx, "txn-pfc").Return(txn, nil)

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-pfc")
	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimaryDisplay, "PFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result.PFCPrimaryDisplay)
	mockRepo.AssertExpectations(t)
}

// TestListTransactions_PopulatesPFCDisplay asserts that list results are enriched.
func TestListTransactions_PopulatesPFCDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	primary := "TRANSPORTATION"
	transactions := []domain.Transaction{
		{ID: "txn-1", UserID: "user-1", PFCPrimary: &primary},
	}
	mockRepo.On("GetByUserIDWithFilters", ctx, "user-1", mock.Anything, 50, 0).Return(transactions, nil)
	mockRepo.On("CountByUserID", ctx, "user-1", mock.Anything).Return(1, nil)

	resp, err := service.ListTransactions(ctx, "user-1", domain.TransactionFilters{}, 50, 0)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)
	require.NotNil(t, resp.Transactions[0].PFCPrimaryDisplay, "PFCPrimaryDisplay should be populated")
	assert.Equal(t, "Transportation", *resp.Transactions[0].PFCPrimaryDisplay)
	mockRepo.AssertExpectations(t)
}

func TestListTransactions_ReturnsPFCFields(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	primary1 := "TRANSPORTATION"
	detailed1 := "TRANSPORTATION_TAXIS_AND_RIDE_SHARES"
	transactions := []domain.Transaction{
		{ID: "txn-1", UserID: "user-1", PFCPrimary: &primary1, PFCDetailed: &detailed1},
		{ID: "txn-2", UserID: "user-1"},
	}
	mockRepo.On("GetByUserIDWithFilters", ctx, "user-1", mock.Anything, 50, 0).Return(transactions, nil)
	mockRepo.On("CountByUserID", ctx, "user-1", mock.Anything).Return(2, nil)

	resp, err := service.ListTransactions(ctx, "user-1", domain.TransactionFilters{}, 50, 0)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 2)

	require.NotNil(t, resp.Transactions[0].PFCPrimary)
	assert.Equal(t, "TRANSPORTATION", *resp.Transactions[0].PFCPrimary)
	assert.Equal(t, "TRANSPORTATION_TAXIS_AND_RIDE_SHARES", *resp.Transactions[0].PFCDetailed)
	assert.Nil(t, resp.Transactions[1].PFCPrimary)
	mockRepo.AssertExpectations(t)
}

// TestListByBudgetCategory_PopulatesPFCDisplay asserts that transactions returned by
// ListByBudgetCategory have PFC display fields enriched.
func TestListByBudgetCategory_PopulatesPFCDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	category := &domain.BudgetCategory{ID: "cat-1", BudgetID: "budget-1"}
	primary := "FOOD_AND_DRINK"
	transactions := []domain.Transaction{
		{ID: "txn-1", UserID: "user-1", PFCPrimary: &primary},
	}
	now := time.Now()
	mockRepo.On("GetTransactionsForBudgetCategory", ctx, "user-1", category, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), 50, 0).
		Return(transactions, 1, 42.0, nil)

	result, totalCount, totalAmount, err := service.ListByBudgetCategory(ctx, "user-1", category, now, now, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	assert.Equal(t, 42.0, totalAmount)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].PFCPrimaryDisplay, "PFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result[0].PFCPrimaryDisplay)
	mockRepo.AssertExpectations(t)
}

// TestListByBudgetCategory_WrapsRepoError asserts that repository errors are wrapped
// with the expected message prefix.
func TestListByBudgetCategory_WrapsRepoError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	category := &domain.BudgetCategory{ID: "cat-1", BudgetID: "budget-1"}
	now := time.Now()
	mockRepo.On("GetTransactionsForBudgetCategory", ctx, "user-1", category, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), 50, 0).
		Return(nil, 0, 0.0, errors.New("db failure"))

	_, _, _, err := service.ListByBudgetCategory(ctx, "user-1", category, now, now, 50, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list transactions for budget category")
	mockRepo.AssertExpectations(t)
}

// TestGetTransactionByID_PopulatesCustomCategoryDisplay asserts enrichment of custom-category name.
func TestGetTransactionByID_PopulatesCustomCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-uuid-1"
	kind := "custom"
	txn := &domain.Transaction{
		ID: "txn-1", UserID: "user-1",
		CategoryOverrideKind:     &kind,
		CustomCategoryOverrideID: &catID,
	}
	mockRepo.On("GetByID", ctx, "txn-1").Return(txn, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, "user-1", []string{catID}).
		Return(map[string]string{catID: "My Custom"}, nil)

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-1")
	require.NoError(t, err)
	require.NotNil(t, result.CustomCategoryOverrideDisplay)
	assert.Equal(t, "My Custom", *result.CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// ---- ListTransactionsByCursor tests ----

// TestTransactionService_ListTransactionsByCursor_FirstPage verifies the first page (cursor=nil)
// returns transactions with NextCursor set when the repo signals more rows exist.
func TestTransactionService_ListTransactionsByCursor_FirstPage(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}

	tx1 := domain.Transaction{ID: "id-1", UserID: userID, Amount: 10.0, Date: time.Now()}
	tx2 := domain.Transaction{ID: "id-2", UserID: userID, Amount: 20.0, Date: time.Now()}

	// Repo returns the two rows and a cursor pointing past the last row.
	id2UUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
	repoNextCursor := &domain.TransactionCursor{V: 1, K: "date", D: "desc", ID: id2UUID}

	mockRepo.On("GetByUserIDKeyset", ctx, userID, filters, (*domain.TransactionCursor)(nil), 2).
		Return([]domain.Transaction{tx1, tx2}, repoNextCursor, nil, nil)

	result, err := service.ListTransactionsByCursor(ctx, userID, filters, nil, 2)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Transactions, 2)
	assert.NotNil(t, result.NextCursor, "expected NextCursor to be set on first page")
	assert.Equal(t, 2, result.Limit)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactionsByCursor_LastPage verifies that when the repo returns
// nil for the next cursor the service also returns NextCursor=nil.
func TestTransactionService_ListTransactionsByCursor_LastPage(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	id1UUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
	incomingCursor := &domain.TransactionCursor{V: 1, K: "date", D: "desc", ID: id1UUID}
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}

	tx := domain.Transaction{ID: "id-3", UserID: userID, Amount: 5.0, Date: time.Now()}

	// Repo returns one row and nil cursor (end of list).
	mockRepo.On("GetByUserIDKeyset", ctx, userID, filters, incomingCursor, 2).
		Return([]domain.Transaction{tx}, (*domain.TransactionCursor)(nil), nil, nil)

	result, err := service.ListTransactionsByCursor(ctx, userID, filters, incomingCursor, 2)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Transactions, 1)
	assert.Nil(t, result.NextCursor, "expected NextCursor to be nil on last page")
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_ListTransactionsByCursor_ValidatesLimit ensures limit > 100 is rejected.
func TestTransactionService_ListTransactionsByCursor_ValidatesLimit(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	_, err := service.ListTransactionsByCursor(ctx, "user-1", domain.TransactionFilters{}, nil, 150)

	assert.Error(t, err)
	assert.Equal(t, ErrInvalidLimit, err)
}

// TestTransactionService_ListTransactionsByCursor_ValidatesSortMatch ensures a cursor whose
// sort key/direction mismatches the current filters is rejected.
func TestTransactionService_ListTransactionsByCursor_ValidatesSortMatch(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	// Cursor was issued for sort-by=date but filters now say sort-by=merchant.
	id1UUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
	cursor := &domain.TransactionCursor{V: 1, K: "date", D: "desc", ID: id1UUID}
	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"}

	_, err := service.ListTransactionsByCursor(ctx, "user-1", filters, cursor, 10)

	assert.Error(t, err)
	assert.Equal(t, ErrCursorSortMismatch, err)
}

// TestTransactionService_ListTransactionsByCursor_PropagatesRepoError ensures repository
// errors are wrapped and surfaced by the service.
func TestTransactionService_ListTransactionsByCursor_PropagatesRepoError(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}
	repoErr := errors.New("db connection lost")

	mockRepo.On("GetByUserIDKeyset", ctx, userID, filters, (*domain.TransactionCursor)(nil), 10).
		Return(nil, (*domain.TransactionCursor)(nil), nil, repoErr)

	_, err := service.ListTransactionsByCursor(ctx, userID, filters, nil, 10)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get transactions by cursor")
	assert.ErrorIs(t, err, repoErr)
	mockRepo.AssertExpectations(t)
}

// TestGetTransactionByID_NilDisplayWhenNoCustomCategory asserts display field is nil for non-custom transactions.
func TestGetTransactionByID_NilDisplayWhenNoCustomCategory(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	primary := "FOOD_AND_DRINK"
	txn := &domain.Transaction{ID: "txn-2", UserID: "user-1", PFCPrimary: &primary}
	mockRepo.On("GetByID", ctx, "txn-2").Return(txn, nil)
	// No GetNamesByIDs call expected — no custom override ID

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-2")
	require.NoError(t, err)
	assert.Nil(t, result.CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestGetTransactionByID_EnrichesEffectiveMerchantName asserts that GetTransactionByID populates
// EffectiveMerchantName from the shared resolver (G15 — single-GET path).
func TestGetTransactionByID_EnrichesEffectiveMerchantName(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	mID := "merchant-uuid-1"
	txn := &domain.Transaction{ID: "txn-emn", UserID: "user-1", MerchantID: &mID}
	mockRepo.On("GetByID", ctx, "txn-emn").Return(txn, nil)
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, "user-1", []string{mID}).
		Return(map[string]string{mID: "Whole Foods Market"}, nil)

	result, err := service.GetTransactionByID(ctx, "user-1", "txn-emn")
	require.NoError(t, err)
	require.NotNil(t, result.EffectiveMerchantName, "EffectiveMerchantName must be populated by GetTransactionByID")
	assert.Equal(t, "Whole Foods Market", *result.EffectiveMerchantName)
	mockRepo.AssertExpectations(t)
}

// TestGetTransactionByID_EnrichMerchant_ResolverError asserts that a resolver failure propagates.
func TestGetTransactionByID_EnrichMerchant_ResolverError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	mID := "merchant-err"
	txn := &domain.Transaction{ID: "txn-err", UserID: "user-1", MerchantID: &mID}
	mockRepo.On("GetByID", ctx, "txn-err").Return(txn, nil)
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, "user-1", []string{mID}).
		Return(nil, errors.New("db unavailable"))

	_, err := service.GetTransactionByID(ctx, "user-1", "txn-err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db unavailable")
	mockRepo.AssertExpectations(t)
}

// TestUpdateTransactionTags_ResponseCarriesEffectiveMerchantName asserts that the mutation
// response carries EffectiveMerchantName (enrichment is transitive via GetTransactionByID — G15).
func TestUpdateTransactionTags_ResponseCarriesEffectiveMerchantName(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	mID := "merchant-tags"
	txn := &domain.Transaction{ID: "txn-tags-emn", UserID: "user-1", MerchantID: &mID}
	tags := []string{"groceries"}

	mockRepo.On("GetByID", ctx, "txn-tags-emn").Return(txn, nil)
	mockRepo.On("UpdateTags", ctx, "txn-tags-emn", "user-1", tags, mock.AnythingOfType("[]domain.TagSource")).Return(nil)
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, "user-1", []string{mID}).
		Return(map[string]string{mID: "Starbucks"}, nil)

	result, err := service.UpdateTransactionTags(ctx, "user-1", "txn-tags-emn", tags)
	require.NoError(t, err)
	require.NotNil(t, result.EffectiveMerchantName, "mutation response must carry EffectiveMerchantName")
	assert.Equal(t, "Starbucks", *result.EffectiveMerchantName)
	mockRepo.AssertExpectations(t)
}

// TestUpdateTransactionCategory_ResponseCarriesEffectiveMerchantName asserts that the mutation
// response carries EffectiveMerchantName (enrichment is transitive via final GetTransactionByID — G15).
func TestUpdateTransactionCategory_ResponseCarriesEffectiveMerchantName(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	mID := "merchant-cat"
	txn := &domain.Transaction{ID: "txn-cat-emn", UserID: "user-1", MerchantID: &mID}

	// GetTransactionByID is called twice: once for ownership check, once to return result.
	mockRepo.On("GetByID", ctx, "txn-cat-emn").Return(txn, nil).Times(2)
	mockRepo.On("UpdateCategory", ctx, "txn-cat-emn", "user-1",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// ResolveEffectiveMerchantNames is called once per GetTransactionByID invocation that has a merchant_id.
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, "user-1", []string{mID}).
		Return(map[string]string{mID: "Amazon"}, nil).Times(2)

	result, err := service.UpdateTransactionCategory(ctx, "user-1", "txn-cat-emn", "clear", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result.EffectiveMerchantName, "mutation response must carry EffectiveMerchantName")
	assert.Equal(t, "Amazon", *result.EffectiveMerchantName)
	mockRepo.AssertExpectations(t)
}

// TestListTransactions_PopulatesCustomCategoryDisplay asserts that list enriches custom-category display.
func TestListTransactions_PopulatesCustomCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-uuid-2"
	kind := "custom"
	transactions := []domain.Transaction{
		{ID: "txn-3", UserID: "user-1", CategoryOverrideKind: &kind, CustomCategoryOverrideID: &catID},
	}
	mockRepo.On("GetByUserIDWithFilters", ctx, "user-1", mock.Anything, 50, 0).Return(transactions, nil)
	mockRepo.On("CountByUserID", ctx, "user-1", mock.Anything).Return(1, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, "user-1", []string{catID}).
		Return(map[string]string{catID: "Travel"}, nil)

	resp, err := service.ListTransactions(ctx, "user-1", domain.TransactionFilters{}, 50, 0)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)
	require.NotNil(t, resp.Transactions[0].CustomCategoryOverrideDisplay)
	assert.Equal(t, "Travel", *resp.Transactions[0].CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestUpdateTransactionTags_PopulatesCustomCategoryDisplay asserts that the returned transaction
// has CustomCategoryOverrideDisplay populated after a tag update (enrichment is transitive via GetTransactionByID).
func TestUpdateTransactionTags_PopulatesCustomCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-uuid-tags"
	kind := "custom"
	txn := &domain.Transaction{
		ID: "txn-tags", UserID: "user-1",
		CategoryOverrideKind:     &kind,
		CustomCategoryOverrideID: &catID,
	}
	tags := []string{"groceries"}

	mockRepo.On("GetByID", ctx, "txn-tags").Return(txn, nil)
	mockRepo.On("UpdateTags", ctx, "txn-tags", "user-1", tags, mock.AnythingOfType("[]domain.TagSource")).Return(nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, "user-1", []string{catID}).
		Return(map[string]string{catID: "Food"}, nil)

	result, err := service.UpdateTransactionTags(ctx, "user-1", "txn-tags", tags)
	require.NoError(t, err)
	require.NotNil(t, result.CustomCategoryOverrideDisplay)
	assert.Equal(t, "Food", *result.CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestUpdateTransactionCategory_PopulatesCustomCategoryDisplay asserts that the returned transaction
// has CustomCategoryOverrideDisplay populated after a category update (enrichment is transitive via final GetTransactionByID).
func TestUpdateTransactionCategory_PopulatesCustomCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-uuid-cat"
	kind := "custom"
	txn := &domain.Transaction{
		ID: "txn-cat", UserID: "user-1",
		CategoryOverrideKind:     &kind,
		CustomCategoryOverrideID: &catID,
	}

	// GetTransactionByID is called twice: once for ownership check, once to return result.
	mockRepo.On("GetByID", ctx, "txn-cat").Return(txn, nil).Times(2)
	// Custom-category ownership validation (#1203): category is owned by the same user.
	mockCategoryRepo.On("GetByID", ctx, catID).
		Return(&domain.CustomCategory{ID: catID, UserID: "user-1"}, nil)
	mockRepo.On("UpdateCategory", ctx, "txn-cat", "user-1",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, "user-1", []string{catID}).
		Return(map[string]string{catID: "Household"}, nil).Times(2)

	result, err := service.UpdateTransactionCategory(ctx, "user-1", "txn-cat", "custom", nil, nil, &catID)
	require.NoError(t, err)
	require.NotNil(t, result.CustomCategoryOverrideDisplay)
	assert.Equal(t, "Household", *result.CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestUpdateTransactionCategory_RejectsForeignCustomCategory asserts a transaction cannot be
// assigned a custom category owned by another user (#1203 security): the service rejects it and
// never calls the repo update.
func TestUpdateTransactionCategory_RejectsForeignCustomCategory(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-foreign"
	txn := &domain.Transaction{ID: "txn-x", UserID: "user-1"}
	mockRepo.On("GetByID", ctx, "txn-x").Return(txn, nil)
	mockCategoryRepo.On("GetByID", ctx, catID).
		Return(&domain.CustomCategory{ID: catID, UserID: "other-user"}, nil)

	_, err := service.UpdateTransactionCategory(ctx, "user-1", "txn-x", "custom", nil, nil, &catID)

	require.Error(t, err)
	mockRepo.AssertNotCalled(t, "UpdateCategory",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestListByBudgetCategory_PopulatesCustomCategoryDisplay asserts enrichment via budget category path.
func TestListByBudgetCategory_PopulatesCustomCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewTransactionService(mockRepo, mockCategoryRepo)

	catID := "cat-uuid-3"
	kind := "custom"
	category := &domain.BudgetCategory{ID: "cat-1", BudgetID: "budget-1"}
	transactions := []domain.Transaction{
		{ID: "txn-4", UserID: "user-1", CategoryOverrideKind: &kind, CustomCategoryOverrideID: &catID},
	}
	now := time.Now()
	mockRepo.On("GetTransactionsForBudgetCategory", ctx, "user-1", category,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), 50, 0).
		Return(transactions, 1, 100.0, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, "user-1", []string{catID}).
		Return(map[string]string{catID: "Dining Out"}, nil)

	result, totalCount, _, err := service.ListByBudgetCategory(ctx, "user-1", category, now, now, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].CustomCategoryOverrideDisplay)
	assert.Equal(t, "Dining Out", *result[0].CustomCategoryOverrideDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestTransactionService_SetReviewed_DelegatesToRepo verifies the service forwards the
// review request to the repository and returns its updated count (Issue #1203).
func TestTransactionService_SetReviewed_DelegatesToRepo(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	ids := []string{"tx-1", "tx-2"}
	mockRepo.On("SetReviewed", ctx, "user-1", ids, true).Return(int64(2), nil)

	n, err := service.SetReviewed(ctx, "user-1", ids, true)

	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	mockRepo.AssertExpectations(t)
}

// TestTransactionService_SetReviewed_EmptyIDs verifies the service short-circuits on an
// empty id slice and never touches the repository (Issue #1203).
func TestTransactionService_SetReviewed_EmptyIDs(t *testing.T) {
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()

	n, err := service.SetReviewed(ctx, "user-1", []string{}, true)

	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	mockRepo.AssertNotCalled(t, "SetReviewed", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ── #1329: enrichAccountForInvestmentItems ────────────────────────────────────
// MockAccountRepository is already defined in account_service_test.go (same package).
// strPtr is also defined there; both are reused here.

// helper: builds a minimal unified-feed filters struct with IncludeInvestmentActivity=true.
func investFilters() domain.TransactionFilters {
	return domain.TransactionFilters{IncludeInvestmentActivity: true}
}

// TestEnrichAccount_HappyPath verifies that an investment ActivityItem receives the
// embedded Account (id/name/nickname/mask) when its account_id is found in active accounts.
func TestEnrichAccount_HappyPath(t *testing.T) {
	ctx := context.Background()
	acctID := uuid.New().String()
	userID := "user-enrich-1"
	mask := "4321"
	nick := "My Brokerage"

	mockTxRepo := new(MockTransactionRepository)
	item := &domain.ActivityItem{
		Source:    domain.FeedSourceInvestment,
		ID:        uuid.New().String(),
		UserID:    userID,
		AccountID: acctID,
		Name:      "Buy AAPL",
		Amount:    100.0,
	}
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{item}, (*domain.TransactionCursor)(nil), nil, nil)

	mockAcctRepo := new(MockAccountRepository)
	mockAcctRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID, repository.AccountFilterOptions{}).Return([]domain.Account{
		{
			ID:       acctID,
			Name:     "Brokerage Account",
			Nickname: &nick,
			Mask:     &mask,
			Status:   domain.AccountStatusActive,
		},
	}, nil)

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))
	svc.SetAccountRepository(mockAcctRepo)

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)

	got := resp.Transactions[0]
	require.NotNil(t, got.Account, "Account must be populated for investment row on happy path")
	assert.Equal(t, acctID, got.Account.ID)
	assert.Equal(t, "Brokerage Account", got.Account.Name)
	require.NotNil(t, got.Account.Nickname)
	assert.Equal(t, "My Brokerage", *got.Account.Nickname)
	require.NotNil(t, got.Account.Mask)
	assert.Equal(t, "4321", *got.Account.Mask)

	mockTxRepo.AssertExpectations(t)
	mockAcctRepo.AssertExpectations(t)
}

// TestEnrichAccount_ActiveChainRelink verifies that when an investment row's account_id
// points to a REPLACED account (absent from the active set), but its user_account_id
// matches an active account, the embedded Account resolves to the active chain account.
func TestEnrichAccount_ActiveChainRelink(t *testing.T) {
	ctx := context.Background()
	replacedAcctID := uuid.New().String()
	activeAcctID := uuid.New().String()
	userAccountID := uuid.New().String()
	userID := "user-enrich-2"
	nick := "Fidelity Brokerage"

	mockTxRepo := new(MockTransactionRepository)
	item := &domain.ActivityItem{
		Source:        domain.FeedSourceInvestment,
		ID:            uuid.New().String(),
		UserID:        userID,
		AccountID:     replacedAcctID,
		UserAccountID: strPtr(userAccountID),
		Name:          "Sell TSLA",
		Amount:        500.0,
	}
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{item}, (*domain.TransactionCursor)(nil), nil, nil)

	// Active account references the same user_account_id; replaced account is absent.
	mockAcctRepo := new(MockAccountRepository)
	mockAcctRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID, repository.AccountFilterOptions{}).Return([]domain.Account{
		{
			ID:            activeAcctID,
			Name:          "Fidelity Account",
			Nickname:      &nick,
			UserAccountID: strPtr(userAccountID),
			Status:        domain.AccountStatusActive,
		},
	}, nil)

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))
	svc.SetAccountRepository(mockAcctRepo)

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)

	got := resp.Transactions[0]
	require.NotNil(t, got.Account, "Account must resolve to active chain account after relink")
	assert.Equal(t, activeAcctID, got.Account.ID, "must resolve to active account, not replaced")
	require.NotNil(t, got.Account.Nickname)
	assert.Equal(t, "Fidelity Brokerage", *got.Account.Nickname)

	mockTxRepo.AssertExpectations(t)
	mockAcctRepo.AssertExpectations(t)
}

// TestEnrichAccount_MissingAccount verifies that when neither account_id nor user_account_id
// resolves to an active account, item.Account stays nil (drawer falls back to "Unknown account").
func TestEnrichAccount_MissingAccount(t *testing.T) {
	ctx := context.Background()
	userID := "user-enrich-3"

	mockTxRepo := new(MockTransactionRepository)
	item := &domain.ActivityItem{
		Source:    domain.FeedSourceInvestment,
		ID:        uuid.New().String(),
		UserID:    userID,
		AccountID: uuid.New().String(), // not in active set
		Name:      "Dividend",
		Amount:    10.0,
	}
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{item}, (*domain.TransactionCursor)(nil), nil, nil)

	mockAcctRepo := new(MockAccountRepository)
	mockAcctRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID, repository.AccountFilterOptions{}).Return([]domain.Account{}, nil) // empty active set

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))
	svc.SetAccountRepository(mockAcctRepo)

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)

	assert.Nil(t, resp.Transactions[0].Account, "Account must remain nil when no active account found")

	mockTxRepo.AssertExpectations(t)
	mockAcctRepo.AssertExpectations(t)
}

// TestEnrichAccount_NilSafe verifies that when SetAccountRepository is NOT called the
// service silently skips enrichment and does not panic (all existing tests are unaffected).
func TestEnrichAccount_NilSafe(t *testing.T) {
	ctx := context.Background()
	userID := "user-enrich-4"

	mockTxRepo := new(MockTransactionRepository)
	item := &domain.ActivityItem{
		Source:    domain.FeedSourceInvestment,
		ID:        uuid.New().String(),
		UserID:    userID,
		AccountID: uuid.New().String(),
		Name:      "Buy MSFT",
		Amount:    200.0,
	}
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{item}, (*domain.TransactionCursor)(nil), nil, nil)

	// No SetAccountRepository call — accountRepo is nil.
	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err, "must not panic or error when accountRepo is nil")
	require.Len(t, resp.Transactions, 1)
	assert.Nil(t, resp.Transactions[0].Account, "Account stays nil when accountRepo not wired")

	mockTxRepo.AssertExpectations(t)
}

// TestListTransactionsByCursor_UnifiedFeed_EnrichesEffectiveMerchantName verifies that
// the default unified feed path (IncludeInvestmentActivity=true) populates
// EffectiveMerchantName for banking rows via ResolveEffectiveMerchantNames (G6 fix).
func TestListTransactionsByCursor_UnifiedFeed_EnrichesEffectiveMerchantName(t *testing.T) {
	ctx := context.Background()
	userID := "user-enrich-merchant-1"
	merchantID := uuid.New().String()

	bankingItem := &domain.ActivityItem{
		Source:     domain.FeedSourceBanking,
		ID:         uuid.New().String(),
		UserID:     userID,
		AccountID:  uuid.New().String(),
		Name:       "WHOLEFDSMRKT #10234",
		MerchantID: &merchantID,
		Amount:     45.0,
	}

	mockTxRepo := new(MockTransactionRepository)
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{bankingItem}, (*domain.TransactionCursor)(nil), nil, nil)
	mockTxRepo.On("ResolveEffectiveMerchantNames", ctx, userID, []string{merchantID}).
		Return(map[string]string{merchantID: "Whole Foods Market"}, nil)

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)

	item := resp.Transactions[0]
	require.NotNil(t, item.EffectiveMerchantName, "EffectiveMerchantName must be populated on unified feed banking rows")
	assert.Equal(t, "Whole Foods Market", *item.EffectiveMerchantName)

	mockTxRepo.AssertExpectations(t)
}

// TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_NoMerchantID verifies that
// banking rows without a merchant_id do not cause a resolve call or panic.
func TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_NoMerchantID(t *testing.T) {
	ctx := context.Background()
	userID := "user-enrich-merchant-2"

	bankingItem := &domain.ActivityItem{
		Source:    domain.FeedSourceBanking,
		ID:        uuid.New().String(),
		UserID:    userID,
		AccountID: uuid.New().String(),
		Name:      "Manual Entry",
		Amount:    10.0,
		// MerchantID intentionally nil
	}

	mockTxRepo := new(MockTransactionRepository)
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{bankingItem}, (*domain.TransactionCursor)(nil), nil, nil)
	// ResolveEffectiveMerchantNames must NOT be called when there are no merchant IDs.

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)
	assert.Nil(t, resp.Transactions[0].EffectiveMerchantName, "nil MerchantID must leave EffectiveMerchantName nil")

	// Verify ResolveEffectiveMerchantNames was never called.
	mockTxRepo.AssertNotCalled(t, "ResolveEffectiveMerchantNames", mock.Anything, mock.Anything, mock.Anything)
	mockTxRepo.AssertExpectations(t)
}

// TestEnrichAccount_BankingScopeGuard verifies that banking rows in the same unified feed
// are NOT given an embedded Account (out-of-scope for #1329).
func TestEnrichAccount_BankingScopeGuard(t *testing.T) {
	ctx := context.Background()
	acctID := uuid.New().String()
	userID := "user-enrich-5"

	mockTxRepo := new(MockTransactionRepository)
	bankingItem := &domain.ActivityItem{
		Source:    domain.FeedSourceBanking,
		ID:        uuid.New().String(),
		UserID:    userID,
		AccountID: acctID,
		Name:      "Coffee Shop",
		Amount:    4.5,
	}
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{bankingItem}, (*domain.TransactionCursor)(nil), nil, nil)

	// No accounts expectation — the enrichment early-exits when there are no
	// investment rows, so GetAccountsWithInstitutionFiltered must NOT be called.
	mockAcctRepo := new(MockAccountRepository)

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))
	svc.SetAccountRepository(mockAcctRepo)

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)

	assert.Nil(t, resp.Transactions[0].Account, "banking rows must NOT receive embedded Account (#1329 scope)")

	mockTxRepo.AssertExpectations(t)
	// Verify the accounts repo was never queried — banking-only feeds must not pay the extra round-trip.
	mockAcctRepo.AssertNotCalled(t, "GetAccountsWithInstitutionFiltered", mock.Anything, mock.Anything, mock.Anything)
}

// ── G10: budget-category list enriches effective merchant name ───────────────

// TestListByBudgetCategory_EnrichesEffectiveMerchantName asserts that the budget-category
// transaction list resolves the effective merchant name via ResolveEffectiveMerchantNames.
func TestListByBudgetCategory_EnrichesEffectiveMerchantName(t *testing.T) {
	ctx := context.Background()
	userID := "user-budget-merchant-1"
	merchantID := uuid.New().String()

	category := &domain.BudgetCategory{ID: "cat-1", BudgetID: "budget-1"}
	rawName := "WHOLEFDSMRKT #10234"
	txns := []domain.Transaction{
		{ID: "txn-1", UserID: userID, MerchantID: &merchantID, MerchantName: &rawName},
	}

	mockRepo := new(MockTransactionRepository)
	mockRepo.On("GetTransactionsForBudgetCategory", ctx, userID, category,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), 50, 0).
		Return(txns, 1, 42.0, nil)
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, userID, []string{merchantID}).
		Return(map[string]string{merchantID: "Whole Foods Market"}, nil)

	svc := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	now := time.Now()
	result, _, _, err := svc.ListByBudgetCategory(ctx, userID, category, now, now, 50, 0)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].EffectiveMerchantName, "EffectiveMerchantName must be set for budget-category txn with a merchant override")
	assert.Equal(t, "Whole Foods Market", *result[0].EffectiveMerchantName)

	mockRepo.AssertExpectations(t)
}

// TestListByBudgetCategory_EnrichMerchant_ResolverError asserts that an error from
// ResolveEffectiveMerchantNames propagates through ListByBudgetCategory.
func TestListByBudgetCategory_EnrichMerchant_ResolverError(t *testing.T) {
	ctx := context.Background()
	userID := "user-budget-merchant-err-1"
	merchantID := uuid.New().String()

	category := &domain.BudgetCategory{ID: "cat-1", BudgetID: "budget-1"}
	rawName := "GARBLED #99"
	txns := []domain.Transaction{
		{ID: "txn-1", UserID: userID, MerchantID: &merchantID, MerchantName: &rawName},
	}

	mockRepo := new(MockTransactionRepository)
	mockRepo.On("GetTransactionsForBudgetCategory", ctx, userID, category,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), 50, 0).
		Return(txns, 1, 10.0, nil)
	mockRepo.On("ResolveEffectiveMerchantNames", ctx, userID, []string{merchantID}).
		Return(nil, errors.New("db error"))

	svc := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))
	now := time.Now()
	_, _, _, err := svc.ListByBudgetCategory(ctx, userID, category, now, now, 50, 0)
	assert.Error(t, err, "resolver error must propagate from ListByBudgetCategory")

	mockRepo.AssertExpectations(t)
}

// ── G11: unified-feed propagates ResolveEffectiveMerchantNames error ─────────

// TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_ResolverError asserts that an
// error from ResolveEffectiveMerchantNames is propagated by ListTransactionsByCursor.
func TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_ResolverError(t *testing.T) {
	ctx := context.Background()
	userID := "user-merchant-err-1"
	merchantID := uuid.New().String()

	bankingItem := &domain.ActivityItem{
		Source:     domain.FeedSourceBanking,
		ID:         uuid.New().String(),
		UserID:     userID,
		AccountID:  uuid.New().String(),
		Name:       "Garbled Name",
		MerchantID: &merchantID,
		Amount:     10.0,
	}

	mockTxRepo := new(MockTransactionRepository)
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{bankingItem}, (*domain.TransactionCursor)(nil), nil, nil)
	mockTxRepo.On("ResolveEffectiveMerchantNames", ctx, userID, []string{merchantID}).
		Return(nil, errors.New("db error"))

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))

	_, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	assert.Error(t, err, "resolver error must propagate from ListTransactionsByCursor")

	mockTxRepo.AssertExpectations(t)
}

// ── G12: absent merchant ID in resolver map leaves EffectiveMerchantName nil ─

// TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_MerchantIDAbsentFromMap asserts
// that a banking item whose merchant_id is missing from the resolver result map does not
// panic and leaves EffectiveMerchantName nil.
func TestListTransactionsByCursor_UnifiedFeed_EnrichMerchant_MerchantIDAbsentFromMap(t *testing.T) {
	ctx := context.Background()
	userID := "user-merchant-absent-1"
	merchantID := uuid.New().String()

	bankingItem := &domain.ActivityItem{
		Source:     domain.FeedSourceBanking,
		ID:         uuid.New().String(),
		UserID:     userID,
		AccountID:  uuid.New().String(),
		Name:       "Unknown Vendor",
		MerchantID: &merchantID,
		Amount:     5.0,
	}

	mockTxRepo := new(MockTransactionRepository)
	mockTxRepo.On("GetByUserIDKeysetUnified", ctx, userID, investFilters(), (*domain.TransactionCursor)(nil), 50).
		Return([]*domain.ActivityItem{bankingItem}, (*domain.TransactionCursor)(nil), nil, nil)
	// Resolver returns an empty map — merchant ID not found.
	mockTxRepo.On("ResolveEffectiveMerchantNames", ctx, userID, []string{merchantID}).
		Return(map[string]string{}, nil)

	svc := NewTransactionService(mockTxRepo, new(MockCustomCategoryRepository))

	resp, err := svc.ListTransactionsByCursor(ctx, userID, investFilters(), nil, 50)
	require.NoError(t, err)
	require.Len(t, resp.Transactions, 1)
	assert.Nil(t, resp.Transactions[0].EffectiveMerchantName, "absent merchant ID must leave EffectiveMerchantName nil — no panic")

	mockTxRepo.AssertExpectations(t)
}
