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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// 2a. The PRIOR 'active' interval was CLOSED, and closed exactly where the new one opens.
	//     uq_ash_open proves at most one open interval implicitly; assert it directly, because
	//     "one removed row exists" is equally true of a table that left the active interval open
	//     and now silently claims the account was in two states at once.
	var activeValidTo, removedValidFrom *time.Time
	var removedValidTo *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT valid_to FROM account_status_history WHERE account_id = $1 AND status = 'active'`,
		closedID).Scan(&activeValidTo))
	require.NotNil(t, activeValidTo, "the prior active interval must be closed, not left open")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT valid_from, valid_to FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		closedID).Scan(&removedValidFrom, &removedValidTo))
	assert.True(t, activeValidTo.Equal(*removedValidFrom),
		"no gap, no overlap: active.valid_to (%v) must equal removed.valid_from (%v)",
		activeValidTo, removedValidFrom)
	assert.Nil(t, removedValidTo, "the removed interval is the current one and must stay open")

	// 2b. Migration 000153: the recorded `reason` must NOT be a false claim about CAUSE. The
	//     trigger derives it from the status transition alone and cannot tell a bank closure from
	//     a user disconnect, so it records the neutral 'removed'. Before 000153 this exact write
	//     was stamped 'user_disconnect' — a plainly false statement about a Plaid-initiated
	//     removal, sitting inside the very table this branch exists to make trustworthy.
	var reason *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT reason FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		closedID).Scan(&reason))
	require.NotNil(t, reason)
	assert.Equal(t, "removed", *reason)
	assert.NotEqual(t, "user_disconnect", *reason,
		"the history table must not claim a bank-initiated closure was a user disconnect")

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

// ---------------------------------------------------------------------------
// Fix round 2: auto-restore, against real Postgres.
//
// The service-level tests can only prove the service honours what the repository answers. The
// authorisation itself is a SQL predicate — `status = 'removed' AND removed_reason =
// 'provider_absent'` — and only these tests can reach it. The user-disconnect case below is the one
// that matters: resurrecting an account the user deliberately disconnected would invert the whole
// principle of Issue #1569.
// ---------------------------------------------------------------------------

// restoreFixture is one user + one linked item + one account, with the account's removal already
// performed through the real production path named by `removal`.
type restoreFixture struct {
	pool        *pgxpool.Pool
	accountRepo repository.AccountRepository
	auditRepo   repository.AuditLogRepository
	userID      string
	item        *domain.LinkedItem
	accountID   string
	plaidID     string
}

func newRestoreFixture(t *testing.T, tag string) *restoreFixture {
	t.Helper()
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	accountRepo := repository.NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%s-%d", tag, time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))

	plaidID := "plaid-acct-" + suffix
	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Restore Fixture', 'depository')
		RETURNING id`, item.ID, user.ID, plaidID).Scan(&accountID))

	return &restoreFixture{
		pool:        pool,
		accountRepo: accountRepo,
		auditRepo:   repository.NewAuditLogRepository(pool),
		userID:      user.ID,
		item:        item,
		accountID:   accountID,
		plaidID:     plaidID,
	}
}

func (f *restoreFixture) service() *transactionSyncService {
	return &transactionSyncService{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo: f.accountRepo,
		auditLogger: f.auditRepo,
	}
}

// retained reads the item's accounts the way the sync does, so these tests exercise the same
// active/removed split production uses.
func (f *restoreFixture) retained(t *testing.T) []domain.Account {
	t.Helper()
	accounts, err := f.accountRepo.GetByLinkedItemIDIncludingRetained(context.Background(), f.item.ID)
	require.NoError(t, err)
	return accounts
}

func (f *restoreFixture) statusAndReason(t *testing.T) (string, *string) {
	t.Helper()
	var status string
	var reason *string
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT status, removed_reason FROM accounts WHERE id = $1`, f.accountID).Scan(&status, &reason))
	return status, reason
}

// rosterWith is a confirmed-complete roster naming this fixture's account plus a filler, so the
// removal side's all-or-nothing bound is never what a restore test is measuring.
func (f *restoreFixture) rosterWith(present bool) PlaidAccountRoster {
	accounts := []plaid.AccountBase{{AccountId: "plaid-filler"}}
	if present {
		accounts = append(accounts, plaid.AccountBase{AccountId: f.plaidID})
	}
	return PlaidAccountRosterFrom(accounts, nil)
}

// TestRestore_Integration_ProviderRemovedAccountReappears is the happy path end to end: the
// removal is performed by the real provider path, the account comes back, and it returns to
// 'active' with its own history interval and a distinct audit action.
func TestRestore_Integration_ProviderRemovedAccountReappears(t *testing.T) {
	f := newRestoreFixture(t, "restore-provider")
	ctx := context.Background()

	require.NoError(t, f.accountRepo.MarkRemovedByProvider(ctx, f.accountID, f.userID))
	status, reason := f.statusAndReason(t)
	require.Equal(t, "removed", status)
	require.NotNil(t, reason)
	require.Equal(t, "provider_absent", *reason, "the provider path must record its own cause")

	restored := f.service().restoreProviderRemovedAccounts(ctx, f.item, f.rosterWith(true), f.retained(t))
	require.Equal(t, 1, restored)

	status, reason = f.statusAndReason(t)
	assert.Equal(t, "active", status)
	assert.Nil(t, reason, "removed_reason must be cleared on restore")

	var removedAt *time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT removed_at FROM accounts WHERE id = $1`, f.accountID).Scan(&removedAt))
	assert.Nil(t, removedAt, "a live account must not carry a removal timestamp")

	// The restore is its own interval: active -> removed -> active, with only the last one open.
	var intervals int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1`, f.accountID).Scan(&intervals))
	assert.Equal(t, 3, intervals)

	var openStatus string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT status FROM account_status_history WHERE account_id = $1 AND valid_to IS NULL`,
		f.accountID).Scan(&openStatus))
	assert.Equal(t, "active", openStatus)

	var auditCount int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = $1 AND resource_id = $2`,
		auditActionAccountRestoredByProvider, f.accountID).Scan(&auditCount))
	assert.Equal(t, 1, auditCount)
}

// TestRestore_Integration_UserDisconnectedAccountIsNeverRestored is the dangerous direction, and
// the only test that can reach the SQL predicate that enforces it. The removal is performed through
// the real user path (UpdateStatus, as DisconnectAccount calls it), the bank keeps reporting the
// account, and it must STAY removed.
func TestRestore_Integration_UserDisconnectedAccountIsNeverRestored(t *testing.T) {
	f := newRestoreFixture(t, "restore-user")
	ctx := context.Background()

	require.NoError(t, f.accountRepo.UpdateStatus(ctx, f.accountID, f.userID, domain.AccountStatusRemoved, nil))
	status, reason := f.statusAndReason(t)
	require.Equal(t, "removed", status)
	require.NotNil(t, reason)
	require.Equal(t, "user_disconnect", *reason, "the user path must record its own cause")

	restored := f.service().restoreProviderRemovedAccounts(ctx, f.item, f.rosterWith(true), f.retained(t))
	assert.Equal(t, 0, restored, "a user disconnect must never be undone by a sync")

	status, reason = f.statusAndReason(t)
	assert.Equal(t, "removed", status, "the account the user disconnected must still be removed")
	require.NotNil(t, reason)
	assert.Equal(t, "user_disconnect", *reason)

	// No phantom interval, and nothing in the trail claiming a restore happened.
	var openStatus string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT status FROM account_status_history WHERE account_id = $1 AND valid_to IS NULL`,
		f.accountID).Scan(&openStatus))
	assert.Equal(t, "removed", openStatus)

	var auditCount int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = $1 AND resource_id = $2`,
		auditActionAccountRestoredByProvider, f.accountID).Scan(&auditCount))
	assert.Equal(t, 0, auditCount)

	// ⚠ AND NOW WITHOUT THE SERVICE. Since fix round 3 the service recognises the cause itself and
	// never calls the repository for this account, so everything above would also pass if the SQL
	// predicate were deleted. Call the repository DIRECTLY — bypassing the service filter exactly
	// as a future caller with a differently-shaped account read would — and require the refusal to
	// come from the database. This assertion, not the ones above, is the guarantee.
	err := f.accountRepo.RestoreProviderRemovedAccount(ctx, f.accountID, f.userID)
	assert.ErrorIs(t, err, repository.ErrAccountNotRestorable,
		"the SQL gate must refuse a user disconnect even when nothing filtered it first")

	status, reason = f.statusAndReason(t)
	assert.Equal(t, "removed", status, "the direct call must not have changed anything either")
	require.NotNil(t, reason)
	assert.Equal(t, "user_disconnect", *reason)
}

// TestRestore_Integration_NullRemovedReasonIsNotRestorable pins the backfill decision: every
// 'removed' row written before migration 000154 has a NULL cause, and NULL means "not
// provider-removed", hence not eligible. The safe direction, chosen because those rows predate the
// provider path entirely and were therefore all user disconnects.
func TestRestore_Integration_NullRemovedReasonIsNotRestorable(t *testing.T) {
	f := newRestoreFixture(t, "restore-null")
	ctx := context.Background()

	// Exactly what a pre-000154 row looks like: removed, with no recorded cause.
	_, err := f.pool.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = NULL WHERE id = $1`,
		f.accountID)
	require.NoError(t, err)

	restored := f.service().restoreProviderRemovedAccounts(ctx, f.item, f.rosterWith(true), f.retained(t))
	assert.Equal(t, 0, restored)

	status, reason := f.statusAndReason(t)
	assert.Equal(t, "removed", status)
	assert.Nil(t, reason)

	// And the repository refuses it directly, with the sentinel that says "declined", not "failed".
	err = f.accountRepo.RestoreProviderRemovedAccount(ctx, f.accountID, f.userID)
	assert.ErrorIs(t, err, repository.ErrAccountNotRestorable)
}

// TestRestore_Integration_PartialRosterRestoresNothing: an account reappearing in a response that
// cannot certify anything proves nothing.
func TestRestore_Integration_PartialRosterRestoresNothing(t *testing.T) {
	f := newRestoreFixture(t, "restore-partial")
	ctx := context.Background()

	require.NoError(t, f.accountRepo.MarkRemovedByProvider(ctx, f.accountID, f.userID))

	errored := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: f.plaidID}}, errors.New("plaid: 500"))
	assert.Equal(t, 0, f.service().restoreProviderRemovedAccounts(ctx, f.item, errored, f.retained(t)))

	status, _ := f.statusAndReason(t)
	assert.Equal(t, "removed", status)
}

// TestRestore_Integration_RemovalThenRestoreRoundTrip proves the two halves compose: the same sync
// machinery removes an account when Plaid drops it and restores it when Plaid brings it back, with
// current net worth following in both directions. This is the whole point of fix round 2 — the
// removal is no longer permanent.
func TestRestore_Integration_RemovalThenRestoreRoundTrip(t *testing.T) {
	f := newRestoreFixture(t, "restore-roundtrip")
	ctx := context.Background()

	// A second, always-present account: the removal side's all-or-nothing bound would otherwise
	// (correctly) refuse to remove the only account of the item.
	var survivorID string
	require.NoError(t, f.pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Survivor', 'depository')
		RETURNING id`, f.item.ID, f.userID, "plaid-survivor-"+f.plaidID).Scan(&survivorID))

	_, err := f.pool.Exec(ctx, `
		INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, NULL, $3, 400, 'USD', NOW(), 'sync')`,
		uuid.New(), f.accountID, f.userID)
	require.NoError(t, err)

	dashboard := NewDashboardService(repository.NewBalanceSnapshotRepository(f.pool), f.accountRepo)
	before, err := dashboard.GetNetWorth(ctx, f.userID, nil)
	require.NoError(t, err)
	require.InDelta(t, 400.0, before.TotalAssets, 0.001)

	svc := f.service()
	active := func() []domain.Account {
		accounts, err := f.accountRepo.GetByLinkedItemID(ctx, f.item.ID)
		require.NoError(t, err)
		return accounts
	}

	// Sync 1: Plaid drops the account.
	dropped := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-survivor-" + f.plaidID}}, nil)
	require.Equal(t, 1, svc.reconcileRemovedAccounts(ctx, f.item, dropped, active()))
	mid, err := dashboard.GetNetWorth(ctx, f.userID, nil)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, mid.TotalAssets, 0.001, "the closed account leaves current net worth")

	// Sync 2: Plaid returns it again.
	back := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "plaid-survivor-" + f.plaidID},
		{AccountId: f.plaidID},
	}, nil)
	require.Equal(t, 1, svc.restoreProviderRemovedAccounts(ctx, f.item, back, f.retained(t)))

	after, err := dashboard.GetNetWorth(ctx, f.userID, nil)
	require.NoError(t, err)
	assert.InDelta(t, 400.0, after.TotalAssets, 0.001,
		"a wrongly-removed account must be able to come back — the removal is not permanent")

	// The whole story is in the history table, in order, with one open interval.
	rows, err := f.pool.Query(ctx,
		`SELECT status FROM account_status_history WHERE account_id = $1 ORDER BY valid_from`, f.accountID)
	require.NoError(t, err)
	defer rows.Close()
	var seq []string
	for rows.Next() {
		var st string
		require.NoError(t, rows.Scan(&st))
		seq = append(seq, st)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"active", "removed", "active"}, seq)
}
