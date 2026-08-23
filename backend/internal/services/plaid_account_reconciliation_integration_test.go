//go:build integration

package services

// plaid_account_reconciliation_integration_test.go — Issue #1569, Task 5, against REAL Postgres.
//
// The unit tests (plaid_account_reconciliation_test.go) prove the decision logic: which accounts
// reconciliation touches, and — far more importantly — which ones it refuses to touch. They cannot
// prove the three consequences that only exist in the database:
//
//  1. the soft-remove write path actually stamps removed_at and fires migration 000152's history
//     trigger, closing the 'active' interval and opening a 'removed' one — exactly ONE of them;
//  2. the account then DROPS OUT of current net worth, which is the whole point (a bank-closed
//     account used to sit there with a frozen balance, inflating the current number forever);
//  3. its balance_snapshots survive, because this branch's entire premise is that removal retains
//     history rather than destroying it.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestReconcileRemovedAccounts_Integration -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestReconcileRemovedAccounts_Integration_PlaidDroppedAccountLeavesCurrentNetWorth is brief test 1
// end to end: two live accounts, Plaid's next /accounts/get names only one of them, and the other
// must stop counting.
func TestReconcileRemovedAccounts_Integration_PlaidDroppedAccountLeavesCurrentNetWorth(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	accountRepo := repository.NewAccountRepository(pool, enc)
	snapshotRepo := repository.NewBalanceSnapshotRepository(pool)
	auditRepo := repository.NewAuditLogRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_plaiddeath_" + suffix,
		Email:              "plaiddeath-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	// context.Background(), not t.Context(): a cancelled cleanup context silently leaks fixture
	// rows into other packages' tests on the shared integration DB.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-plaiddeath-" + suffix,
		AccessToken: "access-token-plaiddeath-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))

	insertAccount := func(name, plaidAccountID string) string {
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
			VALUES ($1, $2, $3, $4, 'depository')
			RETURNING id`,
			item.ID, user.ID, plaidAccountID, name).Scan(&id))
		return id
	}
	seedSnapshot := func(accountID string, balance float64) {
		_, err := pool.Exec(ctx, `
			INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
			VALUES ($1, $2, NULL, $3, $4, 'USD', NOW(), 'sync')`,
			uuid.New(), accountID, user.ID, balance)
		require.NoError(t, err, "seed balance snapshot")
	}

	liveID := insertAccount("Still Open Checking", "plaid-live-"+suffix)
	closedID := insertAccount("Bank Closed Savings", "plaid-closed-"+suffix)
	seedSnapshot(liveID, 1000)
	seedSnapshot(closedID, 250)

	dashboard := NewDashboardService(snapshotRepo, accountRepo)
	before, err := dashboard.GetNetWorth(ctx, user.ID, nil)
	require.NoError(t, err)
	require.InDelta(t, 1250.0, before.TotalAssets, 0.001,
		"precondition: both accounts count toward current net worth before reconciliation")

	svc := &transactionSyncService{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo: accountRepo,
		auditLogger: auditRepo,
	}

	internalAccounts, err := accountRepo.GetByLinkedItemID(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, internalAccounts, 2)

	// Plaid's response names ONLY the still-open account: the bank closed the other one.
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live-" + suffix}}, nil)
	removed := svc.reconcileRemovedAccounts(ctx, item, roster, internalAccounts)
	require.Equal(t, 1, removed)

	// 1. The closed account is soft-removed, with removed_at stamped (migration 000149's CHECK
	//    would have rejected the row otherwise).
	var status string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, closedID).Scan(&status, &removedAt))
	assert.Equal(t, "removed", status)
	require.NotNil(t, removedAt)

	// The still-open account is untouched.
	var liveStatus string
	var liveRemovedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, liveID).Scan(&liveStatus, &liveRemovedAt))
	assert.Equal(t, "active", liveStatus)
	assert.Nil(t, liveRemovedAt)

	// 2. Exactly ONE 'removed' history interval was recorded by the trigger — and none at all for
	//    the account that is still alive.
	var historyCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		closedID).Scan(&historyCount))
	assert.Equal(t, 1, historyCount)

	var liveHistoryCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		liveID).Scan(&liveHistoryCount))
	assert.Equal(t, 0, liveHistoryCount)

	// 3. The removal is audit-logged, so "why did this account disappear?" has an answer that
	//    distinguishes a bank closure from the user's own disconnect.
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = $1 AND resource_type = 'account' AND resource_id = $2`,
		auditActionAccountRemovedByProvider, closedID).Scan(&auditCount))
	assert.Equal(t, 1, auditCount)

	// 4. THE POINT: it drops out of CURRENT net worth...
	after, err := dashboard.GetNetWorth(ctx, user.ID, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1000.0, after.TotalAssets, 0.001,
		"the bank-closed account must stop inflating current net worth")

	// ...while its history survives, which is this whole branch's premise.
	var snapshotCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM balance_snapshots WHERE account_id = $1`, closedID).Scan(&snapshotCount))
	assert.Equal(t, 1, snapshotCount, "soft-removal must retain the account's financial history")
}

// TestReconcileRemovedAccounts_Integration_ZeroAccountResponseChangesNothing is the catastrophic
// case at the database level: a degraded /accounts/get that enumerated nothing must leave every
// row, every history interval and the user's net worth exactly as they were. A unit test proves no
// repository call is made; this proves the database is untouched even if one somehow were.
func TestReconcileRemovedAccounts_Integration_ZeroAccountResponseChangesNothing(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	accountRepo := repository.NewAccountRepository(pool, enc)
	snapshotRepo := repository.NewBalanceSnapshotRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_zerosweep_" + suffix,
		Email:              "zerosweep-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-zerosweep-" + suffix,
		AccessToken: "access-token-zerosweep-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))

	for i, name := range []string{"Checking", "Savings", "Brokerage"} {
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
			VALUES ($1, $2, $3, $4, 'depository')
			RETURNING id`,
			item.ID, user.ID, fmt.Sprintf("plaid-zerosweep-%d-%s", i, suffix), name).Scan(&id))
		_, err := pool.Exec(ctx, `
			INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
			VALUES ($1, $2, NULL, $3, 500, 'USD', NOW(), 'sync')`,
			uuid.New(), id, user.ID)
		require.NoError(t, err)
	}

	dashboard := NewDashboardService(snapshotRepo, accountRepo)
	before, err := dashboard.GetNetWorth(ctx, user.ID, nil)
	require.NoError(t, err)
	require.InDelta(t, 1500.0, before.TotalAssets, 0.001)

	svc := &transactionSyncService{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo: accountRepo,
	}
	internalAccounts, err := accountRepo.GetByLinkedItemID(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, internalAccounts, 3)

	removed := svc.reconcileRemovedAccounts(ctx, item, PlaidAccountRosterFrom(nil, nil), internalAccounts)
	assert.Equal(t, 0, removed)

	var activeCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE linked_item_id = $1 AND status = 'active'`,
		item.ID).Scan(&activeCount))
	assert.Equal(t, 3, activeCount, "a zero-account response must not sweep the portfolio")

	var removedHistory int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM account_status_history h
		JOIN accounts a ON a.id = h.account_id
		WHERE a.linked_item_id = $1 AND h.status = 'removed'`, item.ID).Scan(&removedHistory))
	assert.Equal(t, 0, removedHistory)

	after, err := dashboard.GetNetWorth(ctx, user.ID, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1500.0, after.TotalAssets, 0.001, "net worth must be unchanged")
}
