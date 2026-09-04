package services

import (
	"context"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1586 gap G7 — the settlement repair must target the row that EXISTS, not the id this service
// happened to mint.
//
// THE DEFECT, and it lived in the service rather than in the SQL. Both storage paths mint a UUID
// for every transaction before writing it, and CreateBatch's ON CONFLICT DO UPDATE does not assign
// `id`: a row that is already stored keeps the id it was FIRST inserted with. The repair was then
// run against the minted id, which names no row, so it matched nothing and reported success.
//
// The repository half of the fix is pinned against real Postgres by
// TestLateSettlement_RedeliveredTwinIsRepairableByItsPersistedID_Integration, which drives an actual
// ON CONFLICT. These two pin the SERVICE half: that each caller reads the id back out of the write
// it just performed. The modified path could not — it handed CreateBatch a COPY of its transaction,
// so the persisted id was written into a value that was thrown away.
//
// ⚠ THE STAND-IN IS THE REPOSITORY'S PROVEN CONTRACT, NOT A GUESS. createBatchConflict emulates
// exactly what the integration test measures: the persisted id is written back into the caller's
// slice, and the returned insert-set stays EMPTY for a conflict row. Both halves matter — the
// second is what keeps EmitBatchSideEffects from re-firing rule analytics on a re-delivery.

// createBatchConflict stubs CreateBatch as a conflict against an already-stored row with id
// persistedID: it overwrites the caller's minted id and reports no inserts.
func createBatchConflict(repo *MockTransactionRepository, persistedID string) {
	repo.On("CreateBatch", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			batch := args.Get(1).([]domain.Transaction)
			for i := range batch {
				batch[i].ID = persistedID
			}
		}).
		Return(map[string]bool{}, nil)
}

// capturedRepairIDs returns the id slices the repair was called with.
func capturedRepairIDs(repo *MockTransactionRepository) [][]string {
	var out [][]string
	for _, call := range repo.Calls {
		if call.Method == "PullForwardSettledPendingAnnotations" {
			out = append(out, call.Arguments.Get(2).([]string))
		}
	}
	return out
}

func TestProcessAddedTransactions_RepairsAgainstThePersistedID(t *testing.T) {
	ctx := context.Background()
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	txn := plaid.Transaction{
		TransactionId: "plaid-posted-g7a", AccountId: "plaid-acc-1", Amount: 12,
		Date: "2024-03-01", Name: "Posted twin",
	}
	pendingID := "plaid-pending-g7a"
	txn.PendingTransactionId = *plaid.NewNullableString(&pendingID)

	createBatchConflict(mockTransactionRepo, "stored-row-id-added")
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", ctx, "user-g7a", mock.Anything).
		Return(int64(1), nil)

	require.NoError(t, service.processAddedTransactions(ctx,
		[]plaid.Transaction{txn},
		&domain.LinkedItem{ID: "linked-item-g7a", UserID: "user-g7a"},
		map[string]string{"plaid-acc-1": "account-uuid-1"}, nil))

	assert.Equal(t, [][]string{{"stored-row-id-added"}}, capturedRepairIDs(mockTransactionRepo),
		"the added path must repair against the id the write reported back, not the minted one")
}

// The modified path's residual-(3) branch upserts a transaction that has no live stored row — a row
// tombstoned by an earlier settlement is exactly such a case — and it is the branch that reaches a
// conflict most easily.
func TestProcessModifiedTransactions_RepairsAgainstThePersistedID(t *testing.T) {
	ctx := context.Background()
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	txn := plaid.Transaction{
		TransactionId: "plaid-posted-g7m", AccountId: "plaid-acc-1", Amount: 12,
		Date: "2024-03-01", Name: "Posted twin",
	}
	pendingID := "plaid-pending-g7m"
	txn.PendingTransactionId = *plaid.NewNullableString(&pendingID)

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-posted-g7m", "user-g7m").
		Return((*domain.Transaction)(nil), nil)
	createBatchConflict(mockTransactionRepo, "stored-row-id-modified")
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", ctx, "user-g7m", mock.Anything).
		Return(int64(1), nil)

	require.NoError(t, service.processModifiedTransactions(ctx,
		[]plaid.Transaction{txn},
		&domain.LinkedItem{ID: "linked-item-g7m", UserID: "user-g7m"},
		map[string]string{"plaid-acc-1": "account-uuid-1"}, nil))

	assert.Equal(t, [][]string{{"stored-row-id-modified"}}, capturedRepairIDs(mockTransactionRepo),
		"the modified path handed CreateBatch a COPY, so the persisted id was written into a value "+
			"that was immediately discarded and the repair chased a phantom")
}
