//go:build integration

package repository

// #1569 task 10.1 — the CARDINALITY of the re-link backstop.
//
// supersedeStrandedChainSiblings (account_repository.go) is the write that records WHICH live row
// replaced WHICH. Everything downstream — accountChainLiveCTE's rationing, live holdings, the four
// investment-analytics SUMs — reads what it wrote. Two ways to get the cardinality wrong, and both
// are money:
//
//   - too FEW distinct successors recorded (item A): the rationing counts count(DISTINCT succ_id),
//     so N predecessors all pointing at ONE successor ration away only ONE of them and the rest are
//     summed beside their own replacements. That is the original doubling, reached through the
//     mitigation written to fix it.
//   - too MANY predecessors retired (item E): a batch carrying ONE row for a chain that has TWO
//     live maskless siblings retired BOTH, and the second one is a live account the user still
//     holds. Historical rationing keeps such a sibling on a transition day; nothing restores it to
//     CURRENT-state reads. That is the only failure in this family that loses a live account.
//
// The rule the tests below pin: order the chain's candidates (active first, then rows a fail-open
// re-link left 'replaced' with no pointer), pair candidate r with successor r, and touch nothing
// past the number of successors this batch actually brought.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedChainSibling adds one more account on the same item and chain key, in the given status.
func seedChainSibling(
	ctx context.Context, t *testing.T, db DB,
	userID, linkedItemID, userAccountID uuid.UUID, prefix, status string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Sibling', 'depository', 'active', $5)`,
		id, linkedItemID, userID, prefix+"-sib-"+id.String(), userAccountID)
	require.NoError(t, err)
	if status != "active" {
		_, err = db.Exec(ctx,
			`UPDATE accounts SET status = $2, replaced_at = NOW() WHERE id = $1`, id, status)
		require.NoError(t, err)
	}
	return id
}

func newChainAccount(userID, linkedItemID, userAccountID uuid.UUID, prefix string) domain.Account {
	id := uuid.New().String()
	ua := userAccountID.String()
	return domain.Account{
		ID:             id,
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		UserAccountID:  &ua,
		PlaidAccountID: prefix + "-new-" + id,
		Name:           "New",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
}

// TestAccountRepository_CreateBatch_TwoPredecessorsTwoSuccessors_RecordDISTINCTSuccessors is item A.
//
// The loop-per-new-account form ran the same `status = 'active'` UPDATE once per successor: the
// FIRST iteration flipped BOTH predecessors and pointed both at successor #1, and the second
// iteration matched nothing because 'active' was already false. One successor recorded for two
// predecessors, permanently — so count(DISTINCT succ_id) is 1, the rationing excludes exactly one
// predecessor, and the other is summed alongside B1 + B2.
func TestAccountRepository_CreateBatch_TwoPredecessorsTwoSuccessors_RecordDISTINCTSuccessors(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, a1 := seedRelinkChainFixture(ctx, t, tx, "card-pair")
	a2 := seedChainSibling(ctx, t, tx, userID, linkedItemID, userAccountID, "card-pair", "active")

	b1 := newChainAccount(userID, linkedItemID, userAccountID, "card-pair")
	b2 := newChainAccount(userID, linkedItemID, userAccountID, "card-pair")
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{b1, b2}))

	s1, p1, _ := accountStatusRow(ctx, t, tx, a1)
	s2, p2, _ := accountStatusRow(ctx, t, tx, a2)
	assert.Equal(t, string(domain.AccountStatusReplaced), s1)
	assert.Equal(t, string(domain.AccountStatusReplaced), s2)
	require.NotNil(t, p1)
	require.NotNil(t, p2)
	assert.NotEqual(t, *p1, *p2,
		"both predecessors pointing at ONE successor makes count(DISTINCT succ_id) = 1, so the "+
			"rationing in accountChainLiveCTE excludes only ONE of them and the other is summed "+
			"beside B1 + B2 — the $9,000-for-$5,000 defect, through its own mitigation")
	assert.ElementsMatch(t, []string{b1.ID, b2.ID}, []string{*p1, *p2},
		"each predecessor must be paired with a successor this batch actually created")
}

// TestAccountRepository_CreateBatch_OneSuccessorMayNotRetireTwoLiveSiblings is item E, and it is a
// regression this branch introduced rather than a hole it left open.
//
// Two LEGITIMATE maskless siblings — a brokerage and an IRA at one broker, which MatchOrCreate
// hands the SAME user_account_id — are both live. A batch bringing ONE row for that chain retired
// BOTH of them. The second is an account the user still holds: it vanishes from live holdings
// (holding_repository.go) and from all four SUM(a.current_balance) aggregates, and the historical
// rationing that keeps a sibling on a transition day cannot put it back into current state.
//
// At most as many predecessors may be retired as this batch brought successors for the chain.
func TestAccountRepository_CreateBatch_OneSuccessorMayNotRetireTwoLiveSiblings(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, a1 := seedRelinkChainFixture(ctx, t, tx, "card-solo")
	a2 := seedChainSibling(ctx, t, tx, userID, linkedItemID, userAccountID, "card-solo", "active")

	b1 := newChainAccount(userID, linkedItemID, userAccountID, "card-solo")
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{b1}))

	s1, p1, _ := accountStatusRow(ctx, t, tx, a1)
	s2, p2, _ := accountStatusRow(ctx, t, tx, a2)
	statuses := []string{s1, s2}
	assert.ElementsMatch(t,
		[]string{string(domain.AccountStatusReplaced), string(domain.AccountStatusActive)}, statuses,
		"one successor may retire exactly one predecessor. Retiring both deletes a live account "+
			"from the user's dashboard, and no historical rule can restore it")

	// The one that was retired points at the successor; the survivor points at nothing.
	if s1 == string(domain.AccountStatusReplaced) {
		require.NotNil(t, p1)
		assert.Equal(t, b1.ID, *p1)
		assert.Nil(t, p2, "a still-active sibling must not carry a successor pointer")
	} else {
		require.NotNil(t, p2)
		assert.Equal(t, b1.ID, *p2)
		assert.Nil(t, p1, "a still-active sibling must not carry a successor pointer")
	}
}

// TestAccountRepository_CreateBatch_FillsThePointerLeftNullByAFailedItemWrite is item B, topology
// 7a: the two fail-open writes in account_service step 5b are INDEPENDENT, and the shape nobody
// covered is the one where the item Update fails but the account UpdateStatus SUCCEEDS.
//
// The predecessor is then 'replaced' with a NULL replaced_by_id (UpdateStatus passes nil), the old
// item points at nothing, and the backstop's `status = 'active'` filter matches nothing at all. No
// item pointer, no account pointer, no succession row: the predecessor and its successor are both
// summed, on a plain 1:1 chain. This is the likeliest single-write failure of the two, and it was
// the uncovered one.
func TestAccountRepository_CreateBatch_FillsThePointerLeftNullByAFailedItemWrite(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, a1 := seedRelinkChainFixture(ctx, t, tx, "card-7a")
	// The account status write LANDED; the item write did not. This is exactly what
	// UpdateStatus(..., AccountStatusReplaced, nil) leaves behind.
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'replaced', replaced_at = NOW(), replaced_by_id = NULL WHERE id = $1`, a1)
	require.NoError(t, err)

	b1 := newChainAccount(userID, linkedItemID, userAccountID, "card-7a")
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{b1}))

	s1, p1, _ := accountStatusRow(ctx, t, tx, a1)
	assert.Equal(t, string(domain.AccountStatusReplaced), s1, "already replaced; the status is not the gap")
	require.NotNil(t, p1,
		"a predecessor left 'replaced' with no pointer is invisible to BOTH succession routes, so "+
			"it is summed beside the successor that replaced it. The backstop is the only writer "+
			"that can still record the fact, and the row is already retired — filling the pointer "+
			"changes no current-state read")
	assert.Equal(t, b1.ID, *p1)
}

// TestAccountRepository_CreateBatch_PointerFillDoesNotStealASuccessorFromALiveSibling keeps the two
// halves above from fighting: a chain holding one ACTIVE predecessor and one pointerless 'replaced'
// one, with a single successor, must retire the live one — that is the row whose duplication is
// visible in current state today — rather than spending the only successor slot on a row that is
// already retired.
func TestAccountRepository_CreateBatch_PointerFillDoesNotStealASuccessorFromALiveSibling(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, active := seedRelinkChainFixture(ctx, t, tx, "card-mixed")
	stranded := seedChainSibling(ctx, t, tx, userID, linkedItemID, userAccountID, "card-mixed", "replaced")
	_, err = tx.Exec(ctx, `UPDATE accounts SET replaced_by_id = NULL WHERE id = $1`, stranded)
	require.NoError(t, err)

	b1 := newChainAccount(userID, linkedItemID, userAccountID, "card-mixed")
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{b1}))

	s, p, _ := accountStatusRow(ctx, t, tx, active)
	assert.Equal(t, string(domain.AccountStatusReplaced), s,
		"the ACTIVE duplicate is the one doubling every current-state read; it takes the slot")
	require.NotNil(t, p)
	assert.Equal(t, b1.ID, *p)

	var activeRows int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE user_account_id = $1 AND status = 'active'`, userAccountID).
		Scan(&activeRows))
	assert.Equal(t, 1, activeRows, "exactly the successor remains live on the chain")
}
