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

// TestSyncTransactions_BlankNextCursor_DoesNotOverwriteStoredCursor pins the LIVE path against a
// blank cursor write. Issue #1537 round 9.
//
// WHY THIS IS NOT A DUPLICATE OF AN EXISTING GUARD. Round 4 added exactly this check to
// workers/plaid_sync_provider.go — but `NewPlaidSyncProvider` has **no non-test caller** (its own
// comprehensive test says so at plaid_sync_provider_comprehensive_test.go:777), so the guard went
// into dead code while the path that actually runs kept the bug. The live path is
// transactionSyncService.SyncTransactions, reached from PlaidSyncWorker (queue/jobs/plaid_sync.go)
// and wired in cmd/server/main.go.
//
// THE FAILURE THIS PREVENTS, traced rather than assumed: an empty stored cursor makes
// SyncTransactions take the no-cursor branch on the next run (the `if cursor == ""` historical-depth
// lookup), and plaid_service.go then sends the request with initial-sync historical options and no
// cursor. Plaid replays the whole configured historical window through CreateBatch — so a single
// blank write converts every subsequent incremental sync into a full re-sync.
//
// ⚠ The blank write must stay legal on ONE path: the PlaidErrorInvalidCursor branch resets the
// cursor to "" deliberately, to force exactly that full re-sync. TestSyncTransactions_InvalidCursor…
// pins it, and it must keep passing — which is why the guard belongs at the per-page update site and
// NOT inside UpdateCursor.
func TestSyncTransactions_BlankNextCursor_DoesNotOverwriteStoredCursor(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo — unreached: a non-empty cursor skips the historical-depth lookup
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	goodCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &goodCursor,
	}

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").
		Return([]domain.Account{{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"}}, nil)

	// A successful page that carries NO next_cursor. This is the shape under test.
	resp := &plaid.TransactionsSyncResponse{
		Added:    []plaid.Transaction{},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	resp.SetNextCursor("")
	resp.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", goodCursor, 0).Return(resp, nil)

	// Permissive so an unexpected call is a readable ASSERTION failure rather than a mock panic.
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockLinkedItemRepo.AssertNotCalled(t, "UpdateCursor", ctx, "linked-item-123", "")

	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "UpdateCursor" {
			assert.NotEmpty(t, c.Arguments.Get(2),
				"UpdateCursor was called with an EMPTY cursor, overwriting %q. Plaid sent no "+
					"next_cursor on a successful page; persisting that blank makes the next sync take "+
					"the no-cursor branch and replay the item's entire historical window.", goodCursor)
		}
	}
}
