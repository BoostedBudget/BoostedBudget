package jobs

// Issue #1609 Item 7: the lock transaction must NOT span the Plaid sync.
//
// A production DLQ entry exhausted all 5 attempts with:
//
//	failed to commit transaction: FATAL: terminating connection due to
//	idle-in-transaction timeout (SQLSTATE 25P03)
//
// Root cause: all three sync workers opened a transaction purely to take a
// `SELECT ... FOR UPDATE` row lock on linked_items, then ran the ENTIRE multi-page
// Plaid sync (HTTP + every DB write, all on OTHER pool connections) before
// committing. The tx connection sat idle-in-transaction for the whole sync, so
// Postgres eventually killed it and the failure surfaced at Commit.
//
// Why dropping the long hold is safe — the lock protected far less than it looked:
//   - The WEBHOOK path takes no lock at all. transactionSyncService.ProcessWebhook
//     does a plain GetByItemID and calls SyncTransactions directly
//     (transaction_sync_service.go:359-371). So a webhook-driven sync could always
//     run concurrently with a job-driven one; the row lock never prevented that.
//   - Worker-vs-worker serialization is already provided by River's uniqueness
//     (ByArgs + ByState including Running — see plaidSyncUniqueOpts in
//     services/account_service.go:35-47).
//
// So the lock only ever serialized worker-vs-worker, which River already does.
// Committing immediately after the read keeps the ownership check and the brief
// row-lock read while removing the idle window entirely.
//
// These tests pin ORDERING, not just "commit happened": a test that only asserted
// Commit was called would pass against the buggy code, which also commits — just
// far too late.

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// recordingTx records the order of Commit/Rollback relative to other events by
// appending to a shared slice owned by the test.
type recordingTx struct {
	pgx.Tx
	events *[]string
}

func (t *recordingTx) Commit(ctx context.Context) error {
	*t.events = append(*t.events, "tx.Commit")
	return nil
}

func (t *recordingTx) Rollback(ctx context.Context) error {
	// Rollback after a successful Commit is a documented no-op in the worker's
	// defer; record it so an accidental double-commit ordering is visible.
	*t.events = append(*t.events, "tx.Rollback")
	return nil
}

// recordingBeginner hands out recordingTx values and records Begin.
type recordingBeginner struct {
	events *[]string
}

func (b *recordingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	*b.events = append(*b.events, "tx.Begin")
	return &recordingTx{events: b.events}, nil
}

// TestPlaidSyncWorker_LockTxCommitsBeforeSync is the core regression test for the
// #1609 DLQ failure. It fails against the pre-fix worker, where Commit runs after
// SyncTransactions returns.
func TestPlaidSyncWorker_LockTxCommitsBeforeSync(t *testing.T) {
	var events []string

	userID := uuid.New().String()
	itemID := uuid.New().String()
	item := &domain.LinkedItem{ID: itemID, UserID: userID}

	linkedItemRepo := new(MockLinkedItemRepository)
	linkedItemRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, itemID).
		Run(func(mock.Arguments) { events = append(events, "GetByIDForUpdate") }).
		Return(item, nil)

	syncSvc := new(MockTransactionSyncService)
	syncSvc.On("SyncTransactions", mock.Anything, item).
		Run(func(mock.Arguments) { events = append(events, "SyncTransactions") }).
		Return(&domain.TransactionSyncResult{}, nil)

	worker := &PlaidSyncWorker{
		syncService:    syncSvc,
		linkedItemRepo: linkedItemRepo,
		db:             &recordingBeginner{events: &events},
		logger:         slog.Default(),
	}

	err := worker.Work(context.Background(), newTestRiverJob(PlaidSyncArgs{UserID: userID, ItemID: itemID}))
	require.NoError(t, err)

	commitIdx := indexOf(events, "tx.Commit")
	syncIdx := indexOf(events, "SyncTransactions")

	require.NotEqual(t, -1, commitIdx, "lock transaction was never committed; events=%v", events)
	require.NotEqual(t, -1, syncIdx, "sync never ran; events=%v", events)

	require.Less(t, commitIdx, syncIdx,
		"the lock transaction MUST commit before SyncTransactions runs, otherwise the "+
			"connection sits idle-in-transaction for the whole multi-page Plaid sync and "+
			"Postgres kills it (SQLSTATE 25P03). events=%v", events)
}

// TestPlaidSyncWorker_OwnershipMismatchStillRejectedBeforeSync pins that shortening
// the transaction did not drop the ownership check that the FOR UPDATE read guarded.
func TestPlaidSyncWorker_OwnershipMismatchStillRejectedBeforeSync(t *testing.T) {
	var events []string

	itemID := uuid.New().String()
	item := &domain.LinkedItem{ID: itemID, UserID: uuid.New().String()} // different owner

	linkedItemRepo := new(MockLinkedItemRepository)
	linkedItemRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, itemID).Return(item, nil)

	syncSvc := new(MockTransactionSyncService)
	syncSvc.On("SyncTransactions", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { events = append(events, "SyncTransactions") }).
		Return(&domain.TransactionSyncResult{}, nil)

	worker := &PlaidSyncWorker{
		syncService:    syncSvc,
		linkedItemRepo: linkedItemRepo,
		db:             &recordingBeginner{events: &events},
		logger:         slog.Default(),
	}

	err := worker.Work(context.Background(), newTestRiverJob(PlaidSyncArgs{
		UserID: uuid.New().String(), // does not own the item
		ItemID: itemID,
	}))

	require.NoError(t, err, "ownership mismatch is a non-retryable skip, not an error")
	require.Equal(t, -1, indexOf(events, "SyncTransactions"),
		"sync must not run for an item the job's user does not own; events=%v", events)
}

func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}
