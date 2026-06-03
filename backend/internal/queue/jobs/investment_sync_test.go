package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockInvestmentSyncService mocks the InvestmentSyncService interface.
type MockInvestmentSyncService struct {
	mock.Mock
}

func (m *MockInvestmentSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

func (m *MockInvestmentSyncService) SyncHoldings(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.HoldingSyncResult, error) {
	args := m.Called(ctx, linkedItem)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HoldingSyncResult), args.Error(1)
}

func (m *MockInvestmentSyncService) SyncInvestmentTransactions(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.InvestmentTransactionSyncResult, error) {
	args := m.Called(ctx, linkedItem)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InvestmentTransactionSyncResult), args.Error(1)
}

func (m *MockInvestmentSyncService) SetAnalytics(_ phclient.Analytics) {}

// ─── Holdings Worker Tests ────────────────────────────────────────────────────

func newTestHoldingsJob(args InvestmentHoldingsSyncArgs) *river.Job[InvestmentHoldingsSyncArgs] {
	return &river.Job[InvestmentHoldingsSyncArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestInvestmentHoldingsSyncArgs_Kind(t *testing.T) {
	assert.Equal(t, "investment_holdings_sync", InvestmentHoldingsSyncArgs{}.Kind())
}

func TestInvestmentHoldingsSyncWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:                   itemID,
		UserID:               userID,
		AccessTokenEncrypted: "encrypted-token",
	}

	holdingResult := &domain.HoldingSyncResult{
		SecuritiesUpserted: 3,
		HoldingsReplaced:   5,
		AccountsProcessed:  2,
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncHoldings", ctx, linkedItem).Return(holdingResult, nil)

	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestHoldingsJob(InvestmentHoldingsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestInvestmentHoldingsSyncWorker_Work_LinkedItemNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, repository.ErrLinkedItemNotFound)

	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestHoldingsJob(InvestmentHoldingsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Should NOT return error (don't retry for deleted items)
	require.NoError(t, err)
	mockSvc.AssertNotCalled(t, "SyncHoldings")
}

func TestInvestmentHoldingsSyncWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, errors.New("db connection error"))

	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestHoldingsJob(InvestmentHoldingsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock on linked item")
	mockSvc.AssertNotCalled(t, "SyncHoldings")
}

func TestInvestmentHoldingsSyncWorker_Work_TemporaryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{ID: itemID, UserID: userID}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncHoldings", ctx, linkedItem).Return(nil, errors.New("RATE_LIMIT_EXCEEDED"))

	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestHoldingsJob(InvestmentHoldingsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Temporary error → retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "investment holdings sync failed")
}

func TestInvestmentHoldingsSyncWorker_Work_PermanentError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{ID: itemID, UserID: userID}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	plaidErr := &domain.PlaidSyncError{
		OriginalError: errors.New("access token invalid"),
		ErrorType:     domain.PlaidErrorPermanent,
		ErrorCode:     "INVALID_ACCESS_TOKEN",
	}
	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncHoldings", ctx, linkedItem).Return(nil, plaidErr)

	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestHoldingsJob(InvestmentHoldingsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Permanent error → no retry
	require.NoError(t, err)
}

func TestInvestmentHoldingsSyncUniqueOpts(t *testing.T) {
	opts := InvestmentHoldingsSyncUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 5*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
}

func TestNewInvestmentHoldingsSyncWorker_DefaultLogger(t *testing.T) {
	worker := NewInvestmentHoldingsSyncWorker(InvestmentHoldingsSyncWorkerDeps{
		InvestmentSyncSvc: new(MockInvestmentSyncService),
		LinkedItemRepo:    new(MockLinkedItemRepository),
	})
	assert.NotNil(t, worker.logger)
}

// ─── Transactions Worker Tests ────────────────────────────────────────────────

func newTestTransactionsJob(args InvestmentTransactionsSyncArgs) *river.Job[InvestmentTransactionsSyncArgs] {
	return &river.Job[InvestmentTransactionsSyncArgs]{
		JobRow: &rivertype.JobRow{
			ID:      2,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestInvestmentTransactionsSyncArgs_Kind(t *testing.T) {
	assert.Equal(t, "investment_transactions_sync", InvestmentTransactionsSyncArgs{}.Kind())
}

func TestInvestmentTransactionsSyncWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:                   itemID,
		UserID:               userID,
		AccessTokenEncrypted: "encrypted-token",
	}

	txnResult := &domain.InvestmentTransactionSyncResult{
		PagesProcessed:      2,
		TransactionsInserted: 45,
		TransactionsSkipped:  5,
		SecuritiesUpserted:  3,
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncInvestmentTransactions", ctx, linkedItem).Return(txnResult, nil)

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestInvestmentTransactionsSyncWorker_Work_LinkedItemNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, repository.ErrLinkedItemNotFound)

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Should NOT return error (don't retry for deleted items)
	require.NoError(t, err)
	mockSvc.AssertNotCalled(t, "SyncInvestmentTransactions")
}

func TestInvestmentTransactionsSyncWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, errors.New("db connection error"))

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock on linked item")
	mockSvc.AssertNotCalled(t, "SyncInvestmentTransactions")
}

func TestInvestmentTransactionsSyncWorker_Work_TemporaryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{ID: itemID, UserID: userID}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncInvestmentTransactions", ctx, linkedItem).Return(nil, errors.New("RATE_LIMIT_EXCEEDED"))

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Temporary error → retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "investment transactions sync failed")
}

func TestInvestmentTransactionsSyncWorker_Work_PermanentError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{ID: itemID, UserID: userID}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	plaidErr := &domain.PlaidSyncError{
		OriginalError: errors.New("item revoked"),
		ErrorType:     domain.PlaidErrorPermanent,
		ErrorCode:     "ITEM_LOGIN_REQUIRED",
	}
	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncInvestmentTransactions", ctx, linkedItem).Return(nil, plaidErr)

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID: userID,
		ItemID: itemID,
	}))

	// Permanent error → no retry
	require.NoError(t, err)
}

func TestInvestmentTransactionsSyncWorker_Work_WithWebhookID(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()
	webhookID := uuid.New().String()

	linkedItem := &domain.LinkedItem{ID: itemID, UserID: userID}
	txnResult := &domain.InvestmentTransactionSyncResult{TransactionsInserted: 10}

	mockRepo := new(MockLinkedItemRepository)
	mockSvc := new(MockInvestmentSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSvc.On("SyncInvestmentTransactions", ctx, linkedItem).Return(txnResult, nil)

	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: mockSvc,
		LinkedItemRepo:    mockRepo,
		Logger:            newTestLogger(),
	})

	err := worker.Work(ctx, newTestTransactionsJob(InvestmentTransactionsSyncArgs{
		UserID:    userID,
		ItemID:    itemID,
		WebhookID: webhookID,
	}))

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSvc.AssertExpectations(t)
}

func TestInvestmentTransactionsSyncUniqueOpts(t *testing.T) {
	opts := InvestmentTransactionsSyncUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 5*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
}

func TestNewInvestmentTransactionsSyncWorker_DefaultLogger(t *testing.T) {
	worker := NewInvestmentTransactionsSyncWorker(InvestmentTransactionsSyncWorkerDeps{
		InvestmentSyncSvc: new(MockInvestmentSyncService),
		LinkedItemRepo:    new(MockLinkedItemRepository),
	})
	assert.NotNil(t, worker.logger)
}
