//go:build integration

package repository

// Integration tests proving Issue #1569's core property: removing an account (or disconnecting its
// institution) no longer destroys its financial history.
//
// WHY THESE MUST RUN AGAINST REAL POSTGRES AND NOT pgxmock. pgxmock cannot prove a row SURVIVES —
// it only replays a canned RowsAffected for whatever query text it matched. Before this issue,
// AccountRepository.Delete was a real `DELETE FROM accounts`, which — prior to migration 000150 —
// CASCADE-deleted balance_snapshots; the only way to prove the NEW code path (UpdateStatus /
// MarkRemovedByLinkedItemID) actually leaves the account row and its balance_snapshots in place is
// to seed both against a real schema and query them back afterward.
//
// Task 2 brief, required assertion 5: "DeleteAccount leaves the account row present with
// status='removed' and removed_at set, and leaves its balance_snapshots intact. Assert the
// snapshots survive — that is the whole point."
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountRepository_.*Removed -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_UpdateStatus_Integration_RemovedPreservesBalanceSnapshots proves
// AccountRepository.UpdateStatus(..., AccountStatusRemoved, nil) — what accountService.DeleteAccount
// now calls instead of Delete — leaves the account row present (status='removed', removed_at set)
// and does not touch its balance_snapshots.
func TestAccountRepository_UpdateStatus_Integration_RemovedPreservesBalanceSnapshots(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-"+suffix).Scan(&accountID))

	var snapshotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 1234.56, 'USD', NOW(), 'manual')
		RETURNING id`,
		accountID, userID).Scan(&snapshotID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	var status string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, accountID,
	).Scan(&status, &removedAt))
	assert.Equal(t, "removed", status, "the account row must survive, transitioned to 'removed'")
	require.NotNil(t, removedAt, "removed_at must be stamped — required by accounts_removed_at_consistency")

	var snapshotStillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM balance_snapshots WHERE id = $1 AND account_id = $2`, snapshotID, accountID,
	).Scan(&snapshotStillThere))
	assert.Equal(t, 1, snapshotStillThere,
		"the balance_snapshot must survive untouched — this is the whole point of the #1569 soft-remove")
}

// TestAccountRepository_MarkRemovedByLinkedItemID_Integration_PreservesBalanceSnapshots proves the
// institution-level disconnect path (accountService.DisconnectInstitution's step 4c): every ACTIVE
// account under a linked item transitions to 'removed' with removed_at set, and its
// balance_snapshots survive.
func TestAccountRepository_MarkRemovedByLinkedItemID_Integration_PreservesBalanceSnapshots(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var checkingID, savingsID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository') RETURNING id`,
		item.ID, userID, "plaid-checking-"+suffix).Scan(&checkingID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Savings', 'depository') RETURNING id`,
		item.ID, userID, "plaid-savings-"+suffix).Scan(&savingsID))

	var snapshotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 500.00, 'USD', NOW(), 'manual') RETURNING id`,
		checkingID, userID).Scan(&snapshotID))

	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, item.ID))

	rows, err := pool.Query(ctx, `SELECT id, status, removed_at FROM accounts WHERE linked_item_id = $1 ORDER BY id`, item.ID)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, status string
		var removedAt *time.Time
		require.NoError(t, rows.Scan(&id, &status, &removedAt))
		assert.Equal(t, "removed", status, "every active account under the item must transition to 'removed'")
		assert.NotNil(t, removedAt, "removed_at must be stamped on every transitioned account")
		count++
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 2, count, "both accounts under the item must have been transitioned")

	var snapshotStillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM balance_snapshots WHERE id = $1 AND account_id = $2`, snapshotID, checkingID,
	).Scan(&snapshotStillThere))
	assert.Equal(t, 1, snapshotStillThere, "the balance_snapshot must survive the institution-level disconnect")
}

// TestAccountRepository_UpdateStatus_Integration_RepeatDisconnectDoesNotMoveRemovedAt is the
// Task 2 regression test: a second UpdateStatus(..., AccountStatusRemoved, nil) call against an
// already-'removed' account must refuse the write (ErrAccountAlreadyRemoved — the idempotency
// sentinel, fix round 1) rather than re-stamping removed_at to now(). Before the guard, the second
// call unconditionally re-set removed_at even though the history trigger correctly wrote no new
// account_status_history row for the no-op status transition — so the "last known balance, as of
// <date>" label silently drifted forward to today every time a user hit disconnect twice.
//
// This test is at the REPOSITORY layer: UpdateStatus itself still refuses to write and returns an
// error sentinel regardless of which status it refuses for. The SERVICE-layer decision to treat
// ErrAccountAlreadyRemoved specifically as idempotent success lives in
// TestDisconnectAccount_Integration_RepeatDisconnectIsIdempotentSuccessWithNoWrite
// (internal/services/account_service_disconnect_idempotent_integration_test.go).
func TestAccountRepository_UpdateStatus_Integration_RepeatDisconnectDoesNotMoveRemovedAt(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-repeat-"+suffix).Scan(&accountID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	var firstRemovedAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT removed_at FROM accounts WHERE id = $1`, accountID).Scan(&firstRemovedAt))

	// Backdate removed_at to prove the SECOND call cannot move it forward. Six months, matching the
	// bug report's "claims it was disconnected today instead of six months ago."
	sixMonthsAgo := time.Now().AddDate(0, -6, 0)
	_, err := pool.Exec(ctx, `UPDATE accounts SET removed_at = $1 WHERE id = $2`, sixMonthsAgo, accountID)
	require.NoError(t, err)

	err = accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil)
	require.ErrorIs(t, err, ErrAccountAlreadyRemoved,
		"a repeat disconnect must refuse the write at the repository layer")

	var removedAtAfterRepeat time.Time
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, accountID).Scan(&status, &removedAtAfterRepeat))
	assert.Equal(t, "removed", status)
	assert.WithinDuration(t, sixMonthsAgo, removedAtAfterRepeat, time.Second,
		"the second disconnect call must NOT move removed_at forward")
}

// TestAccountRepository_UpdateStatus_Integration_DoesNotClobberReplacedChainMetadata is the Task 2
// regression test for the "worse" bug: calling UpdateStatus(..., AccountStatusRemoved, nil) against
// an account that is already 'replaced' (part of a re-link chain) must not overwrite its terminal
// status, and must not null out replaced_at/replaced_by_id.
func TestAccountRepository_UpdateStatus_Integration_DoesNotClobberReplacedChainMetadata(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var oldAccountID, newAccountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Old Checking', 'depository') RETURNING id`,
		item.ID, userID, "plaid-old-"+suffix).Scan(&oldAccountID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'New Checking', 'depository') RETURNING id`,
		item.ID, userID, "plaid-new-"+suffix).Scan(&newAccountID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, oldAccountID, userID, domain.AccountStatusReplaced, &newAccountID))

	var replacedAtBefore time.Time
	var replacedByIDBefore string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT replaced_at, replaced_by_id FROM accounts WHERE id = $1`, oldAccountID,
	).Scan(&replacedAtBefore, &replacedByIDBefore))
	require.Equal(t, newAccountID, replacedByIDBefore)

	err := accountRepo.UpdateStatus(ctx, oldAccountID, userID, domain.AccountStatusRemoved, nil)
	require.ErrorIs(t, err, ErrAccountAlreadyDisconnected,
		"a disconnect against an already-replaced account must be refused")

	var status string
	var replacedAtAfter time.Time
	var replacedByIDAfter *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, replaced_at, replaced_by_id FROM accounts WHERE id = $1`, oldAccountID,
	).Scan(&status, &replacedAtAfter, &replacedByIDAfter))
	assert.Equal(t, "replaced", status, "the terminal 'replaced' status must not be overwritten with 'removed'")
	assert.WithinDuration(t, replacedAtBefore, replacedAtAfter, time.Second)
	require.NotNil(t, replacedByIDAfter, "replaced_by_id must survive — this is the re-link chain metadata")
	assert.Equal(t, newAccountID, *replacedByIDAfter)
}

// TestAccountRepository_UpdateStatus_Integration_ExpiredIsAlsoAHardRefusal is fix round 1's other
// half: an already-'expired' account is a genuinely different transition than 'removed', same as
// already-'replaced', so it must ALSO get ErrAccountAlreadyDisconnected (409), not the idempotency
// sentinel — and its status/updated_at must be left untouched.
func TestAccountRepository_UpdateStatus_Integration_ExpiredIsAlsoAHardRefusal(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Expired Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-expired-"+suffix).Scan(&accountID))

	// Force the account into 'expired'. Deliberately does NOT try to pin updated_at to a specific
	// past instant: migration 000007's update_accounts_updated_at trigger re-stamps updated_at to
	// NOW() on ANY UPDATE, including this seeding one, so the only meaningful assertion is that the
	// REFUSED UpdateStatus call below does not move it again from whatever the trigger just set.
	_, err := pool.Exec(ctx, `UPDATE accounts SET status = 'expired' WHERE id = $1`, accountID)
	require.NoError(t, err)

	var updatedAtBefore time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT updated_at FROM accounts WHERE id = $1`, accountID).Scan(&updatedAtBefore))

	err = accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil)
	require.ErrorIs(t, err, ErrAccountAlreadyDisconnected,
		"an already-expired account must be a hard refusal, not the idempotency case")

	var status string
	var updatedAtAfter time.Time
	var removedAtAfter *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, updated_at, removed_at FROM accounts WHERE id = $1`, accountID,
	).Scan(&status, &updatedAtAfter, &removedAtAfter))
	assert.Equal(t, "expired", status, "the terminal 'expired' status must not be overwritten with 'removed'")
	assert.WithinDuration(t, updatedAtBefore, updatedAtAfter, time.Second,
		"the refused call must not touch updated_at either — it must issue no UPDATE at all")
	assert.Nil(t, removedAtAfter, "removed_at must stay unset — an expired account was never disconnected by a user")
}

// TestAccountRepository_UpdateStatus_Integration_FirstDisconnectStillWritesHistory pins the
// happy path: the FIRST disconnect must still work exactly as before the Task 2 guard, and the
// history trigger (migration 000152, `WHEN (OLD.status IS DISTINCT FROM NEW.status)`) must still
// fire and record the active->removed transition interval.
func TestAccountRepository_UpdateStatus_Integration_FirstDisconnectStillWritesHistory(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-firstdisc-"+suffix).Scan(&accountID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	var status string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, accountID).Scan(&status, &removedAt))
	assert.Equal(t, "removed", status)
	require.NotNil(t, removedAt)

	var historyCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		accountID).Scan(&historyCount))
	assert.Equal(t, 1, historyCount, "the first disconnect must still write its history interval")
}

// TestAccountRepository_Reads_Integration_ReturnRemovedAt is the falsifiability test for the
// write-only-column bug: accounts.removed_at was STAMPED by UpdateStatus/MarkRemovedByLinkedItemID
// but never SELECTed by any read, so domain.Account.RemovedAt was always nil, `omitempty` dropped
// it from the API response, and the frontend's getAccountBalanceAsOfDate precedence
// (removed_at ?? replaced_at ?? updated_at) always fell through to updated_at — a column the
// update_accounts_updated_at trigger bumps on ANY later write. The "as of <date>" label on a
// frozen balance would then drift forward and claim a staler figure was fresher than it is.
//
// It deliberately goes through the REAL repository reads rather than a mock: the frontend tests
// pass today only because their fixtures hand-seed removed_at, which is exactly the blind spot
// that hid this. The updated_at bump below is what makes the assertion non-trivial — with the
// SELECT columns removed, RemovedAt is nil and every assertion here fails.
func TestAccountRepository_Reads_Integration_ReturnRemovedAt(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Frozen Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-removedat-"+suffix).Scan(&accountID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	var stamped time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT removed_at FROM accounts WHERE id = $1`, accountID).Scan(&stamped))

	// Bump updated_at AFTER the removal, the way any later write (a nickname edit, a
	// display-order shuffle, the balance writer) does via the migration-000007 trigger. If a read
	// drops removed_at, the frontend silently labels the frozen balance with THIS timestamp.
	_, err := pool.Exec(ctx, `UPDATE accounts SET nickname = 'touched' WHERE id = $1`, accountID)
	require.NoError(t, err)

	byID, err := accountRepo.GetByIDAndUser(ctx, accountID, userID)
	require.NoError(t, err)
	require.NotNil(t, byID.RemovedAt,
		"GetByIDAndUser must return removed_at — without it the API omits the field entirely")
	assert.WithinDuration(t, stamped, *byID.RemovedAt, time.Second)
	assert.True(t, byID.RemovedAt.Before(byID.UpdatedAt),
		"the fixture must actually exercise the drift: updated_at is now later than removed_at")

	listed, err := accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID,
		AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	var found *domain.Account
	for i := range listed {
		if listed[i].ID == accountID {
			found = &listed[i]
		}
	}
	require.NotNil(t, found, "the removed account must be reachable through the accounts-page read")
	require.NotNil(t, found.RemovedAt,
		"GetAccountsWithInstitutionFiltered backs the accounts page — the surface that renders the "+
			"'as of <date>' label — so it must carry removed_at")
	assert.WithinDuration(t, stamped, *found.RemovedAt, time.Second)
}
