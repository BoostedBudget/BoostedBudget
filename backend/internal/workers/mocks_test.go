package workers

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/events"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/mock"
)

// Ensure mocks implement their interfaces
var (
	_ repository.LinkedItemRepository    = (*MockLinkedItemRepository)(nil)
	_ repository.TransactionRepository   = (*MockTransactionRepository)(nil)
	_ PlaidClient                       = (*MockPlaidClient)(nil)
	_ events.EventEmitter               = (*MockEventEmitter)(nil)
	_ phclient.Analytics                = (*MockAnalytics)(nil)
)

// MockAnalytics is a mock for the Analytics interface
type MockAnalytics struct {
	mock.Mock
}

func (m *MockAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *MockAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *MockAnalytics) Close() {
	m.Called()
}

// MockLinkedItemRepository is a mock for LinkedItemRepository
type MockLinkedItemRepository struct {
	mock.Mock
}

func (m *MockLinkedItemRepository) Create(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, itemID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) UpdateCursor(ctx context.Context, id, cursor string) error {
	args := m.Called(ctx, id, cursor)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Update(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) DeleteByUser(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// Issue #171 - Plaid Re-Link Handling
func (m *MockLinkedItemRepository) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, userID, institutionID, status)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// Issue #668: Plaid Item Management for Cost Optimization - Cleanup tracking methods
func (m *MockLinkedItemRepository) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	args := m.Called(ctx, itemID, cleanupAt)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// Issue #679, #680: Trial expiration - methods for disconnecting items
func (m *MockLinkedItemRepository) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockTransactionRepository is a mock for TransactionRepository
type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) Create(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

func (m *MockTransactionRepository) CreateBatch(ctx context.Context, transactions []domain.Transaction) error {
	args := m.Called(ctx, transactions)
	return args.Error(0)
}

func (m *MockTransactionRepository) Update(ctx context.Context, transaction *domain.Transaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

func (m *MockTransactionRepository) DeleteByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) error {
	args := m.Called(ctx, source, externalID, userID)
	return args.Error(0)
}

func (m *MockTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, accountID)
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	args := m.Called(ctx, source, userID)
	return args.Get(0).([]domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	args := m.Called(ctx, userID, filters, limit, offset)
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

func (m *MockTransactionRepository) UpdateCategory(ctx context.Context, id string, userID string, categoryOverride []string, categorySource domain.CategorySource) error {
	args := m.Called(ctx, id, userID, categoryOverride, categorySource)
	return args.Error(0)
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

// Issue #493: Budget Category Matching Service method
func (m *MockTransactionRepository) GetSpentSumForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, category, tags, matchLogic, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

// Issue #497: Category Preview Service method
func (m *MockTransactionRepository) GetMatchingTransactionStats(ctx context.Context, userID string, categories [][]string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	args := m.Called(ctx, userID, categories, tags, matchLogic, startDate, endDate)
	return args.Int(0), args.Get(1).(float64), args.Error(2)
}

// Issue #499: Transaction-Budget Linking UI
func (m *MockTransactionRepository) GetTransactionsForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	args := m.Called(ctx, userID, category, tags, matchLogic, startDate, endDate, limit, offset)
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

// MockPlaidClient is a mock for Plaid API client
type MockPlaidClient struct {
	mock.Mock
}

func (m *MockPlaidClient) TransactionsSync(ctx context.Context, accessToken string, cursor *string) (*plaid.TransactionsSyncResponse, error) {
	args := m.Called(ctx, accessToken, cursor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.TransactionsSyncResponse), args.Error(1)
}

// MockEventEmitter is a mock for EventEmitter
type MockEventEmitter struct {
	mock.Mock
}

func (m *MockEventEmitter) Emit(ctx context.Context, event events.TransactionEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockEventEmitter) Subscribe(handler events.EventHandler) {
	m.Called(handler)
}

// MockProvider is a mock for TransactionSyncProvider
type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) GetProviderName() domain.TransactionSource {
	args := m.Called()
	return args.Get(0).(domain.TransactionSource)
}

func (m *MockProvider) SyncForLinkedItem(ctx context.Context, linkedItemID string) (int, error) {
	args := m.Called(ctx, linkedItemID)
	return args.Int(0), args.Error(1)
}

func (m *MockProvider) SupportsIncrementalSync() bool {
	args := m.Called()
	return args.Bool(0)
}

// Error constants for tests
var (
	ErrLinkedItemNotFound = repository.ErrLinkedItemNotFound
)
