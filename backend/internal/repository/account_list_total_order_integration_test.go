//go:build integration

package repository

// Issue #1609, fix round 2 (F1) — the account list's ORDER BY was not a TOTAL order, so the order
// two tied rows come back in was left to the planner.
//
//	ORDER BY COALESCE(<effective display_order>, 999999) ASC, a.created_at ASC
//
// CreateBatch stamps ONE `now := time.Now()` onto every row of the batch (account_repository.go),
// and every account of a single Plaid connection is created in one batch. Two such accounts
// therefore have byte-identical created_at, and if neither carries a display_order override both
// collapse to the same 999999 sentinel — a COMPLETE tie, with no further key to break it.
// Postgres is free to return a complete tie in any order, and does: a plan change, an index-only
// scan, a VACUUM that moves a tuple, or plain parallelism can flip it. The user-visible symptom is
// an accounts list that silently reshuffles between refreshes, and any keyset/offset pagination
// built on it is non-deterministic.
//
// The fix appends `a.id ASC`. accounts.id is the primary key, so the ordering becomes total: for
// any two distinct rows there is now a key that separates them.
//
// ⚠ WHY INTEGRATION AND NOT pgxmock. The claim is "Postgres returns these two rows in THIS order".
// pgxmock asserts the SQL string and then replays rows the test author invented in the order the
// test author invented them, so it cannot express, let alone falsify, an ordering claim.
//
// ⚠ WHAT MAKES THIS RED WITHOUT THE FIX, and it is not luck. Measured against this repo's own
// Postgres 17 at migration 187: the plan is a Seq Scan feeding a Sort, the scan emits heap order
// (= insertion order), and the Sort — every comparison returning 0 because the rows are completely
// tied — hands back the REVERSE of what it was given. So the pair inserted low-id-first comes back
// high-id-first, and the pair inserted high-id-first comes back low-id-first. That is the finding
// stated as an experiment: two batches with identical column values, differing only in the order
// two INSERTs were issued in, produce OPPOSITE list orders. Case 1 below is the half that fails
// pre-fix; case 2 is the half that passes pre-fix for the wrong reason and must keep passing.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// The pair is deliberately far apart in id space so "sorted by id" is unmistakable.
const (
	tiedLowID  = "00000000-0000-4000-8000-000000000001"
	tiedHighID = "ffffffff-ffff-4fff-bfff-fffffffffff1"
)

// newTiedPair inserts both accounts in ONE CreateBatch — the production shape for the accounts of
// a single Plaid connection, and the reason their created_at is identical. Neither carries a
// display_order, so both sort on the same COALESCE sentinel. lowFirst chooses the INSERT order,
// which is the only thing that differs between the two cases below.
func newTiedPair(t *testing.T, ctx context.Context, f *overlayFixture, lowFirst bool) {
	t.Helper()
	bal := 100.00
	mk := func(id, name string) domain.Account {
		return domain.Account{
			ID: id, UserID: f.userID.String(), LinkedItemID: f.linkedItemID.String(),
			PlaidAccountID: f.prefix + "-" + name, Name: name, Type: "depository",
			CurrentBalance: &bal, AvailableBalance: &bal, CurrencyCode: "USD",
		}
	}
	batch := []domain.Account{mk(tiedLowID, "tied-low"), mk(tiedHighID, "tied-high")}
	if !lowFirst {
		batch[0], batch[1] = batch[1], batch[0]
	}
	require.NoError(t, f.repo.CreateBatch(ctx, f.userID.String(), batch))

	// The premise, asserted rather than assumed: if CreateBatch ever stops sharing one timestamp
	// across the batch, created_at breaks the tie on its own and this file stops testing anything.
	var hi, lo time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT created_at FROM accounts WHERE id = $1`, tiedHighID).Scan(&hi))
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT created_at FROM accounts WHERE id = $1`, tiedLowID).Scan(&lo))
	require.True(t, hi.Equal(lo),
		"fixture premise: one Plaid connection's accounts share a created_at (got %s vs %s)", hi, lo)

	var hiOrder, loOrder *int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, tiedHighID).Scan(&hiOrder))
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT display_order FROM accounts WHERE id = $1`, tiedLowID).Scan(&loOrder))
	require.Nil(t, hiOrder, "fixture premise: neither row carries a display_order override")
	require.Nil(t, loOrder, "fixture premise: neither row carries a display_order override")
}

func assertIDAscending(t *testing.T, rows []domain.Account) {
	t.Helper()
	require.Len(t, rows, 2)
	assert.Equal(t, tiedLowID, rows[0].ID,
		"two rows tied on display_order AND created_at must be separated by id, not by the planner")
	assert.Equal(t, tiedHighID, rows[1].ID)
}

// ── 1. THE REGRESSION. Inserted low-id-first; pre-fix the Sort hands the pair back reversed. ──
//
// GetAccountsWithInstitutionFiltered is the list the accounts page and the API paginate over.
func TestAccountRepository_FilteredList_TiedRowsGetATotalOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)
	newTiedPair(t, ctx, f, true)

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	assertIDAscending(t, rows)
}

// ── 2. The same content, inserted the other way round, must give the SAME list. ───────────────
//
// This is the property in its general form: list order is a function of the DATA, never of the
// order two INSERTs happened to be issued in. It passes pre-fix only by accident of the reversal
// described in the header, which is precisely why it cannot stand alone.
func TestAccountRepository_FilteredList_OrderDoesNotDependOnInsertOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)
	newTiedPair(t, ctx, f, false)

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	assertIDAscending(t, rows)
}

// ── 3. GET /api/v1/accounts reads a different query with the same defect. ─────────────────────
//
// accountService.GetAccounts goes through GetAccountsWithInstitution, which carried the same
// `ORDER BY a.created_at ASC` with no tiebreak and is just as user-visible.
func TestAccountRepository_AccountsWithInstitution_TiedRowsGetATotalOrder_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)
	newTiedPair(t, ctx, f, true)

	rows, err := f.repo.GetAccountsWithInstitution(ctx, f.userID.String())
	require.NoError(t, err)
	assertIDAscending(t, rows)
}

// ── 4. The property the user actually cares about: no reshuffling between refreshes. ─────────
func TestAccountRepository_FilteredList_OrderIsStableAcrossReads_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)
	newTiedPair(t, ctx, f, true)

	var first []string
	for i := 0; i < 5; i++ {
		rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
		require.NoError(t, err)
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if first == nil {
			first = ids
			continue
		}
		require.Equal(t, first, ids, "the accounts list must not reshuffle between refreshes")
	}
}

// ── 5. The tiebreak is LAST, not first. ──────────────────────────────────────────────────────
//
// An explicit display_order still outranks it, so the fix cannot have quietly replaced the user's
// arrangement with an id sort.
func TestAccountRepository_FilteredList_DisplayOrderStillOutranksTheIDTiebreak_Integration(t *testing.T) {
	f, ctx := newOverlayFixture(t)
	newTiedPair(t, ctx, f, true)

	// Put the HIGH id first explicitly; id ASC alone would answer the other way round.
	require.NoError(t, f.repo.UpdateDisplayOrders(ctx, f.userID.String(), []domain.AccountOrder{
		{AccountID: tiedHighID, DisplayOrder: 0},
		{AccountID: tiedLowID, DisplayOrder: 1},
	}))

	rows, err := f.repo.GetAccountsWithInstitutionFiltered(ctx, f.userID.String(), AccountFilterOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, tiedHighID, rows[0].ID, "the id tiebreak must not outrank an explicit display_order")
	assert.Equal(t, tiedLowID, rows[1].ID)
}
