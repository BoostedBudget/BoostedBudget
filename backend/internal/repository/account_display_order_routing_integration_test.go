//go:build integration

package repository

// Issue #1609, Item 3 (task 3c, Addendum A) — PATCH /api/v1/accounts/reorder silently discarded
// every reorder of a Plaid-linked account.
//
// Task 3b made the account read CHAIN-AUTHORITATIVE:
//
//	ORDER BY COALESCE(CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.display_order
//	                       ELSE a.display_order END, 999999)
//
// while UpdateDisplayOrders still wrote `UPDATE accounts SET display_order` and ONLY that. For a
// chained account the write therefore lands in a column the read no longer looks at, and the new
// order vanishes on the next refresh — the exact "saves, then reverts" defect this batch exists
// to fix, relocated to the reorder endpoint.
//
// ⚠ WHO CAN ACTUALLY REACH IT, corrected in fix round 1 (F1) because the first version of this
// comment said "reachable by an ordinary drag in the UI" and that is FALSE. There is no
// drag-reorder UI: `reorderAccounts` (useAccountCustomization.ts) has zero production callers, and
// there is no dnd / draggable / onDragEnd anywhere in src/features/accounts. The endpoint is
// reachable today by API, MCP and integration clients only.
//
// That does not make the defect less real — the endpoint is published, documented in
// api/openapi.yaml and secured like any other, and a client using it as specified got its write
// silently dropped — but the severity is "a published endpoint does not do what it says", not "a
// user's drag is lost". The word "drag" is kept in the case names and bodies below only as a
// description of the ARRANGEMENT being submitted, never as a claim about a UI gesture.
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
// after any client submits a new arrangement. Both chained, so both go through the new leg.
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

	// The reorder: B is moved above A.
	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accB, DisplayOrder: 0},
		{AccountID: accA, DisplayOrder: 1},
	}))

	rows, err = f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, accB, rows[0].ID, "the reordered account must stay where the caller placed it")
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

// ── 7. TWO ACCOUNTS OF ONE CHAIN IN ONE BATCH. ────────────────────────────────────────────────
//
// #1609 fix round 1, F2. A re-link chain holds MANY accounts rows, and both are renderable
// together: AccountsPage fetches status=all_statuses and "Show closed accounts" surfaces the
// replaced/removed member. A list-shaped reorder payload therefore names both, and both resolve
// to the SAME user_accounts row.
//
// The naive loop writes that row twice and LAST WRITE WINS: the account the caller positioned
// first ends up carrying the other one's order, the pair ties, and the read falls back to
// `a.created_at ASC`. The row does not land where it was placed.
//
// The rule: collapse by resolved target, LOWEST display_order wins. Rationale in
// UpdateDisplayOrders' own doc comment; the property this pins is that the chain lands at the
// topmost position any of its members was given, and that the answer does NOT depend on the order
// the caller happened to serialise the batch in.
func TestAccountRepository_UpdateDisplayOrders_TwoAccountsOfOneChainCollapseToTheLowestOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	first := f.newAccount(t, ctx, "chain-member-first", &uaID)
	second := f.newAccount(t, ctx, "chain-member-second", &uaID)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: first, DisplayOrder: 0},
		{AccountID: second, DisplayOrder: 1},
	}))

	var chainSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaID).Scan(&chainSide))
	require.NotNil(t, chainSide)
	assert.Equal(t, 0, *chainSide,
		"the chain must land at the position of its highest-placed member, not inherit the "+
			"order of whichever sibling the loop happened to write last")
}

// The same claim, batch serialised the other way round. This is what makes the rule
// payload-order-INDEPENDENT rather than merely "the loop stopped overwriting": a first-occurrence
// rule would answer 1 here and 0 above, for a payload describing the same arrangement.
func TestAccountRepository_UpdateDisplayOrders_ChainCollapseDoesNotDependOnBatchOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaID := f.newUserAccount(t, ctx)
	first := f.newAccount(t, ctx, "reversed-first", &uaID)
	second := f.newAccount(t, ctx, "reversed-second", &uaID)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: second, DisplayOrder: 1},
		{AccountID: first, DisplayOrder: 0},
	}))

	var chainSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaID).Scan(&chainSide))
	require.NotNil(t, chainSide)
	assert.Equal(t, 0, *chainSide)
}

// The same account named twice collapses on exactly the same rule — the target is the account row
// rather than a chain row, but "one target, one position" is the same statement.
func TestAccountRepository_UpdateDisplayOrders_DuplicateAccountIDCollapses_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	accountID := f.newAccount(t, ctx, "duplicated-unchained", nil)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accountID, DisplayOrder: 5},
		{AccountID: accountID, DisplayOrder: 2},
	}))

	var accountsSide *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, accountID).Scan(&accountsSide))
	require.NotNil(t, accountsSide)
	assert.Equal(t, 2, *accountsSide)
}

// ⚠ COLLAPSING MUST NOT SKIP VALIDATION. An entry whose order is dropped is still an account id
// the caller submitted, and it must still be proved to exist and to belong to the actor —
// otherwise a batch could smuggle a stranger's id past the ownership check by pairing it with a
// sibling that wins the collapse.
func TestAccountRepository_UpdateDisplayOrders_CollapsedAwayEntryIsStillValidated_Integration(t *testing.T) {
	victim, ctx := newOverlayFixture(t)
	attacker, _ := newOverlayFixture(t)

	victimUA := victim.newUserAccount(t, ctx)
	victimAccount := victim.newAccount(t, ctx, "victim-collapsed", &victimUA)
	attackerAccount := attacker.newAccount(t, ctx, "attacker-own", nil)

	// The attacker's own row wins the collapse on order; the victim's id is the loser.
	err := attacker.repo.UpdateDisplayOrders(ctx, attacker.userID.String(), []domain.AccountOrder{
		{AccountID: attackerAccount, DisplayOrder: 0},
		{AccountID: victimAccount, DisplayOrder: 1},
	})
	require.ErrorIs(t, err, ErrAccountNotFound)

	var attackerSide *int
	require.NoError(t, attacker.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, attackerAccount).Scan(&attackerSide))
	assert.Nil(t, attackerSide, "the whole batch must roll back, winner included")
}

// Two accounts on DIFFERENT chains are two targets and must both be written — the collapse is per
// resolved target, never per batch.
func TestAccountRepository_UpdateDisplayOrders_DistinctChainsAreNotCollapsed_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)

	uaA := f.newUserAccount(t, ctx)
	uaB := f.newUserAccount(t, ctx)
	accA := f.newAccount(t, ctx, "distinct-a", &uaA)
	accB := f.newAccount(t, ctx, "distinct-b", &uaB)

	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: accA, DisplayOrder: 0},
		{AccountID: accB, DisplayOrder: 1},
	}))

	var a, b *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaA).Scan(&a))
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM user_accounts WHERE id = $1`, uaB).Scan(&b))
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.Equal(t, 0, *a)
	assert.Equal(t, 1, *b)
}
