package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func (m *MockInvestmentSyncService) SetCacheInvalidation(_ *cache.Generations) {}

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
		SecuritiesUpserted:           3,
		HoldingsReplaced:             5,
		AccountsProcessed:            2,
		AccountsSkippedNonInvestment: 1,
		HoldingsSkippedNonInvestment: 4,
	}
	beforeSkipped := testutil.ToFloat64(observability.InvestmentHoldingsSynced.WithLabelValues("skipped_non_investment"))

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
	assert.Equal(t, beforeSkipped+4,
		testutil.ToFloat64(observability.InvestmentHoldingsSynced.WithLabelValues("skipped_non_investment")))
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
		PagesProcessed:       2,
		TransactionsInserted: 45,
		TransactionsSkipped:  5,
		SecuritiesUpserted:   3,
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

// TestInvestmentTransactionsSyncWorker_Work_DropsReachTheMetrics is #1631 M4's alerting guard, and
// the reason it exists is that the worker's success path is INDISTINGUISHABLE from a healthy one
// when rows were dropped: the service returns no error, Work returns nil, and River records
// status="success". A permanently pinned investments_last_synced_date is therefore a green job
// forever unless these counters are emitted.
//
// It asserts the DELTA on each label rather than an absolute value, because promauto counters are
// process-global and every other test in this package adds to them.
func TestInvestmentTransactionsSyncWorker_Work_DropsReachTheMetrics(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:                   itemID,
		UserID:               userID,
		AccessTokenEncrypted: "encrypted-token",
	}

	// A lossy run: one row lost to each reason, plus one deliberate exclusion, which must land on
	// its OWN label — an exclusion is the owner's instruction and must never inflate a drop.
	txnResult := &domain.InvestmentTransactionSyncResult{
		PagesProcessed:                        1,
		TransactionsInserted:                  10,
		TransactionsSkipped:                   2,
		TransactionsExcluded:                  7,
		TransactionsDroppedUnmappedAccount:    3,
		TransactionsDroppedUnparsableDate:     4,
		TransactionsDroppedUnresolvedSecurity: 5,
	}
	require.True(t, txnResult.AnyTransactionDropped(), "fixture precondition")

	labels := []string{
		"excluded",
		"dropped_unmapped_account",
		"dropped_unparsable_date",
		"dropped_unresolved_security",
	}
	before := make(map[string]float64, len(labels))
	for _, l := range labels {
		before[l] = testutil.ToFloat64(observability.InvestmentTransactionsSynced.WithLabelValues(l))
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
	require.NoError(t, err,
		"a lossy run is still a successful JOB — which is exactly why the loss has to be visible "+
			"through metrics rather than through the job's status")

	want := map[string]float64{
		"excluded":                    7,
		"dropped_unmapped_account":    3,
		"dropped_unparsable_date":     4,
		"dropped_unresolved_security": 5,
	}
	for _, l := range labels {
		got := testutil.ToFloat64(observability.InvestmentTransactionsSynced.WithLabelValues(l)) - before[l]
		assert.Equal(t, want[l], got,
			"investment_transactions_synced_total{action=%q} must carry the count from the sync "+
				"result; without it a pinned cursor has no signal anywhere in the system", l)
	}
}
