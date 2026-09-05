package services

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 10.3 item A — a same-day provider_absent restore used to get NO balance
// snapshot.
//
// createBalanceSnapshots builds `internalAccounts` (the active subset) BEFORE it runs
// restoreProviderRemovedAccounts, and then builds the plaid_account_id -> account map the snapshot
// loop consults from that same slice. An account the bank dropped and then returned inside ONE
// /accounts/get response is therefore restored to 'active' in the database and skipped by the
// snapshot loop in the same pass: the row is live again but its balance stays whatever it was
// before the removal until some later snapshot event happens to fire. Its dashboard net-worth
// contribution is stale in the meantime, and the balance history has a hole exactly at the moment
// the account came back.
//
// ⚠ The fix must NOT reorder removal and restore — see the ordering comment in
// createBalanceSnapshots. It rebuilds the account view AFTER the restore instead.

// plaidAccountsGetServer serves one /accounts/get response and returns a client pointed at it.
func plaidAccountsGetServer(t *testing.T, accounts []map[string]any) *plaid.APIClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Discarded deliberately: encoding to an httptest ResponseWriter cannot meaningfully
		// fail, and t.Fatal is not usable from a handler goroutine.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts":   accounts,
			"item":       map[string]any{"item_id": "item-1", "institution_id": "ins_test", "webhook": "", "available_products": []string{}, "billed_products": []string{}, "update_type": "background"},
			"request_id": "req-accounts-restore-test",
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfg := plaid.NewConfiguration()
	cfg.UseEnvironment(plaid.Environment(server.URL))
	return plaid.NewAPIClient(cfg)
}

func plaidAccountJSON(plaidAccountID string, current float64) map[string]any {
	return map[string]any{
		"account_id":    plaidAccountID,
		"name":          plaidAccountID,
		"type":          "depository",
		"subtype":       "checking",
		"mask":          "0000",
		"official_name": plaidAccountID,
		"balances": map[string]any{
			"current":                  current,
			"available":                current,
			"iso_currency_code":        "USD",
			"unofficial_currency_code": nil,
			"limit":                    nil,
		},
	}
}

func TestCreateBalanceSnapshots_RestoredInTheSamePassStillGetsASnapshot(t *testing.T) {
	ctx := context.Background()
	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1"}

	live := domain.Account{
		ID: "acct-live", LinkedItemID: "item-1", UserID: "user-1",
		PlaidAccountID: "plaid-live", Status: domain.AccountStatusActive,
	}
	providerAbsent := string(domain.RemovalReasonProviderAbsent)
	back := domain.Account{
		ID: "acct-back", LinkedItemID: "item-1", UserID: "user-1",
		PlaidAccountID: "plaid-back", Status: domain.AccountStatusRemoved,
		RemovedReason: &providerAbsent,
	}

	accountRepo := &MockAccountRepository{}
	snapshotRepo := &MockBalanceSnapshotRepository{}
	plaidSvc := &MockPlaidService{}
	audit := &MockAuditLogger{}

	// First read: the account is still 'removed'. After the restore commits, the function must go
	// back to the database (or otherwise refresh its view) rather than trusting the slice it built
	// before the write.
	accountRepo.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{live, back}, nil).Once()
	restored := back
	restored.Status = domain.AccountStatusActive
	restored.RemovedReason = nil
	accountRepo.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{live, restored}, nil)

	accountRepo.On("RestoreProviderRemovedAccount", mock.Anything, "acct-back", "user-1").Return(nil).Once()
	audit.On("Log", mock.Anything, mock.Anything, "account", "acct-back").Return(nil).Maybe()

	snapshotRepo.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	snapshotRepo.On("Create", mock.Anything, mock.Anything).Return(nil)

	plaidSvc.On("GetClient").Return(plaidAccountsGetServer(t, []map[string]any{
		plaidAccountJSON("plaid-live", 1000),
		plaidAccountJSON("plaid-back", 250),
	}))

	svc := &transactionSyncService{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo:         accountRepo,
		balanceSnapshotRepo: snapshotRepo,
		plaidService:        plaidSvc,
		auditLogger:         audit,
	}

	require.NoError(t, svc.createBalanceSnapshots(ctx, item, "access-token"))

	snapshotted := map[string]bool{}
	for _, call := range snapshotRepo.Calls {
		if call.Method == "Create" {
			snapshotted[call.Arguments.Get(1).(*domain.BalanceSnapshot).AccountID] = true
		}
	}
	assert.True(t, snapshotted["acct-live"], "precondition: the account that was never removed is snapshotted")
	assert.True(t, snapshotted["acct-back"],
		"the account Plaid dropped and returned in the SAME response was restored to 'active' — its balance must be recorded in this pass, not left stale until some later snapshot event")

	accountRepo.AssertExpectations(t)
}

// TestCreateBalanceSnapshots_UserDisconnectedAccountStillGetsNoSnapshot is the other direction, and
// the reason the re-read is guarded on the restore count rather than done unconditionally: a
// user-disconnected account that the bank is still happily reporting must stay out of the snapshot
// loop. Nothing restores it, so nothing re-reads, and it accrues no balance history — which is what
// stops a disconnect quietly resuming data collection.
func TestCreateBalanceSnapshots_UserDisconnectedAccountStillGetsNoSnapshot(t *testing.T) {
	ctx := context.Background()
	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1"}

	live := domain.Account{
		ID: "acct-live", LinkedItemID: "item-1", UserID: "user-1",
		PlaidAccountID: "plaid-live", Status: domain.AccountStatusActive,
	}
	userDisconnect := string(domain.RemovalReasonUserDisconnect)
	gone := domain.Account{
		ID: "acct-gone", LinkedItemID: "item-1", UserID: "user-1",
		PlaidAccountID: "plaid-gone", Status: domain.AccountStatusRemoved,
		RemovedReason: &userDisconnect,
	}

	accountRepo := &MockAccountRepository{}
	snapshotRepo := &MockBalanceSnapshotRepository{}
	plaidSvc := &MockPlaidService{}

	accountRepo.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{live, gone}, nil).Once()
	snapshotRepo.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	snapshotRepo.On("Create", mock.Anything, mock.Anything).Return(nil)
	plaidSvc.On("GetClient").Return(plaidAccountsGetServer(t, []map[string]any{
		plaidAccountJSON("plaid-live", 1000),
		plaidAccountJSON("plaid-gone", 250),
	}))

	svc := &transactionSyncService{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo:         accountRepo,
		balanceSnapshotRepo: snapshotRepo,
		plaidService:        plaidSvc,
	}

	require.NoError(t, svc.createBalanceSnapshots(ctx, item, "access-token"))

	snapshotted := map[string]bool{}
	reads := 0
	for _, call := range snapshotRepo.Calls {
		if call.Method == "Create" {
			snapshotted[call.Arguments.Get(1).(*domain.BalanceSnapshot).AccountID] = true
		}
	}
	for _, call := range accountRepo.Calls {
		if call.Method == "GetByLinkedItemIDIncludingRetained" {
			reads++
		}
	}

	assert.True(t, snapshotted["acct-live"])
	assert.False(t, snapshotted["acct-gone"],
		"a user-disconnected account must accrue no balance snapshots even while the bank still reports it")
	assert.Equal(t, 1, reads, "no restore happened, so the account view is not re-read")
	// RestoreProviderRemovedAccount carries no expectation at all: any call is a hard failure.
	accountRepo.AssertExpectations(t)
}
