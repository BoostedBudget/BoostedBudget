//go:build integration

package repository

// Issue #1609, Item 3 (task 3c, Addendum A) — PATCH /api/v1/accounts/reorder silently discards
// every drag-reorder of a Plaid-linked account.
//
// Task 3b made the account read CHAIN-AUTHORITATIVE:
//
//	ORDER BY COALESCE(CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.display_order
//	                       ELSE a.display_order END, 999999)
//
// while UpdateDisplayOrders still wrote `UPDATE accounts SET display_order` and ONLY that. For a
// chained account the write therefore lands in a column the read no longer looks at, and the new
// order vanishes on the next refresh — the exact "saves, then reverts" defect this batch exists
// to fix, relocated to the reorder endpoint. Unlike PATCH /accounts/{id}, the frontend has no
// user_account_id branch here (useAccountCustomization.ts reorderAccounts calls the accounts
// endpoint unconditionally), so this one is reachable by an ordinary drag in the UI.
//
// The fix is SINGLE-SOURCE PER ACCOUNT, not a dual write: user_accounts.display_order when the
// account is chained, accounts.display_order otherwise — the same "source of truth follows the
// chain" rule the read implements, inside the one transaction the method already opens.
//
// ⚠ WHY INTEGRATION AND NOT pgxmock. The claim is "the value the reorder wrote is the value the
// list read gives back", which is a question about which column Postgres wrote and which column
// the read's CASE selected. pgxmock asserts the SQL string and then returns rows the test author
// invented, so it can express neither half.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ── 1. THE REGRESSION. ────────────────────────────────────────────────────────────────────────
//
// A reorder of a CHAINED account must survive a re-read through the list endpoint's own query.
// This is the assertion that fails on the branch as task 3b left it.
func TestAccountRepository_UpdateDisplayOrders_ChainedAccountSurvivesReRead_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "chained-reorder", &uaID)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 3},
	}))

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].DisplayOrder, "the reorder must be visible through the chain-authoritative read")
	assert.Equal(t, 3, *rows[0].DisplayOrder)
}

// ── 2. The user-visible symptom: the list actually comes back in the new order. ───────────────
//
// Case 1 pins the value; this pins the ORDER BY that value feeds, which is what the user sees
// after a drag. Both chained, so both go through the new leg.
func TestAccountRepository_UpdateDisplayOrders_ChainedAccountsComeBackInTheNewOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaA := f.newUserAccount(t, ctx)
	uaB := f.newUserAccount(t, ctx)
	accA := f.newAccount(t, ctx, "alpha", &uaA)
	accB := f.newAccount(t, ctx, "bravo", &uaB)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accA, DisplayOrder: 0},
		{AccountID: accB, DisplayOrder: 1},
	}))
	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, accA, rows[0].ID, "fixture: the starting order must be A, B")

	// The drag: B is moved above A.
	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accB, DisplayOrder: 0},
		{AccountID: accA, DisplayOrder: 1},
	}))

	rows, err = f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, accB, rows[0].ID, "the dragged account must stay where the user dropped it")
	assert.Equal(t, accA, rows[1].ID)
}

// ── 3. The no-regression half. ────────────────────────────────────────────────────────────────
//
// accounts.user_account_id is nullable and the startup repair skips no-match and decryption-
// failure rows, so unchained accounts exist. They must keep writing (and reading) the accounts
// column exactly as before.
func TestAccountRepository_UpdateDisplayOrders_UnchainedAccountStillWritesAccounts_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	accountID := f.newAccount(t, ctx, "unchained-reorder", nil)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 5},
	}))

	var accountsSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, accountID).Scan(&accountsSide))
	require.NotNil(t, accountsSide, "an unchained account keeps its accounts-side order")
	assert.Equal(t, 5, *accountsSide)

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].DisplayOrder)
	assert.Equal(t, 5, *rows[0].DisplayOrder)
}

// ── 4. Single-source, not dual-write. ─────────────────────────────────────────────────────────
//
// The chained write must land on the chain row and must NOT also stamp the legacy accounts
// column. Writing both would re-create the split-brain this batch is removing: the two would
// disagree the moment anything wrote just one of them, and nothing would say which was right.
func TestAccountRepository_UpdateDisplayOrders_ChainedWriteDoesNotTouchTheLegacyColumn_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	accountID := f.newAccount(t, ctx, "single-source", &uaID)
	// A legacy accounts-side value, written by the writer that made legacy values exist.
	require.NoError(t, f.repo.Update(ctx, &domain.Account{
		ID: accountID, DisplayOrder: intptr(42),
	}, f.userID.String()))

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 7},
	}))

	var accountsSide, chainSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, accountID).Scan(&accountsSide))
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaID).Scan(&chainSide))

	require.NotNil(t, chainSide, "the chain row is the source of truth for a chained account")
	assert.Equal(t, 7, *chainSide)
	require.NotNil(t, accountsSide)
	assert.Equal(t, 42, *accountsSide, "the legacy column must be left alone, not dual-written")
}

// ── 5. Ownership and atomicity survive the split. ─────────────────────────────────────────────
//
// An id the actor does not own (or that does not exist) still returns ErrAccountNotFound, and the
// transaction still rolls back the orders that preceded it in the same batch.
func TestAccountRepository_UpdateDisplayOrders_UnknownAccountRollsBackTheBatch_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	chained := f.newAccount(t, ctx, "batch-chained", &uaID)
	unchained := f.newAccount(t, ctx, "batch-unchained", nil)

	err := f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: chained, DisplayOrder: 1},
		{AccountID: unchained, DisplayOrder: 2},
		{AccountID: "00000000-0000-0000-0000-000000000000", DisplayOrder: 3},
	})
	require.ErrorIs(t, err, ErrAccountNotFound)

	var chainSide, accountsSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaID).Scan(&chainSide))
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, unchained).Scan(&accountsSide))
	assert.Nil(t, chainSide, "the chain write must roll back with the rest of the batch")
	assert.Nil(t, accountsSide, "the accounts write must roll back with the rest of the batch")
}

// ── 6. Cross-tenant ids are refused on the chained leg too. ───────────────────────────────────
//
// The pre-3c statement was `WHERE id = $3 AND user_id = $4`. Routing the write through
// user_accounts must not lose that predicate — a chained account whose id belongs to somebody
// else must match nothing rather than reorder a stranger's chain.
func TestAccountRepository_UpdateDisplayOrders_CrossTenantChainedAccountRefused_Integration(t *testing.T) {
	victim, ctx := newOverlayFixture(t)
	attacker, _ := newOverlayFixture(t)

	uaID := victim.newUserAccount(t, ctx)
	accountID := victim.newAccount(t, ctx, "victim-chained", &uaID)

	err := attacker.repo.UpdateDisplayOrders(ctx, attacker.userID.String(), []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 9},
	})
	require.ErrorIs(t, err, ErrAccountNotFound)

	var chainSide *int
	require.NoError(t, victim.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaID).Scan(&chainSide))
	assert.Nil(t, chainSide, "a stranger's chain must be untouched")
}
