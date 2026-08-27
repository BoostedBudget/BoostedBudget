//go:build integration

package repository

// #1569 task 10.1 — the re-link topologies that had NO test, and the two divergences between the
// Upsert's and the repair's readings of "the successor was observed".
//
// Every fixture here uses DISTINCT dollar values per account. An earlier fixture in this family
// gave both predecessors $1,500, so it could not tell WHICH of the two survived — and a defect hid
// behind exactly that symmetry.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func todayUTCFromDB(ctx context.Context, t *testing.T, db DB) time.Time {
	t.Helper()
	return dayUTC(ctx, t, db, 0)
}

// seedChainAccount inserts one more investment account on the given item and chain key, active.
func seedChainAccount(
	ctx context.Context, t *testing.T, db DB, userID, itemID uuid.UUID, chainKey *uuid.UUID, label string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, $5, 'investment', 'active', $6)`,
		id, itemID, userID, label+"-"+id.String(), label, chainKey)
	require.NoError(t, err)
	return id
}

// seedChainItem inserts one more linked item for the user.
func seedChainItem(ctx context.Context, t *testing.T, db DB, userID uuid.UUID, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, id, userID, label+"-"+id.String())
	require.NoError(t, err)
	return id
}

func replaceAccount(ctx context.Context, t *testing.T, db DB, id uuid.UUID, by *uuid.UUID) {
	t.Helper()
	_, err := db.Exec(ctx,
		`UPDATE accounts SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`, id, by)
	require.NoError(t, err)
}

func seedChainHolding(ctx context.Context, t *testing.T, db DB, userID, accountID, secID uuid.UUID, basis, value float64) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, $5, $6)`, uuid.New(), userID, accountID, secID, basis, value)
	require.NoError(t, err)
}

func chainOf(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID) (itemID, chainKey uuid.UUID) {
	t.Helper()
	require.NoError(t, db.QueryRow(ctx,
		`SELECT linked_item_id, user_account_id FROM accounts WHERE id = $1`, accountID).Scan(&itemID, &chainKey))
	return
}

// ── item D: a chain whose edges are of MIXED pointer type ─────────────────────────────────────

// TestPortfolioSnapshot_Upsert_MixedPointerChain_IsTransitivelyClosed is item D.
//
// A → B is recorded ONLY by the item pointer (the account write failed for that link); B → C ONLY
// by the account pointer (the item write failed for that one). Each pointer type closes
// transitively WITHIN itself, but the two could not be combined: A's item closure stopped at B, B
// was excluded by C, and A survived to be summed beside C. The file's claim that "reading either
// pointer closes this window" was too broad — the account-pointer fallback was DIRECT, not
// transitive, and a chain does not have to use one pointer type throughout.
func TestPortfolioSnapshot_Upsert_MixedPointerChain_IsTransitivelyClosed(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// A → B via the ITEM pointer, from the shared helper.
	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-mixedptr")
	_, chainKey := chainOf(ctx, t, tx, accountA)
	itemC := seedChainItem(ctx, t, tx, userID, "psnap-mixedptr-item3")
	accountC := seedChainAccount(ctx, t, tx, userID, itemC, &chainKey, "psnap-mixedptr-c")

	// B → C via the ACCOUNT pointer ONLY: B's item still points at nothing.
	replaceAccount(ctx, t, tx, accountB, &accountC)

	seedChainHolding(ctx, t, tx, userID, accountA, secID, 1000, 4000)
	seedChainHolding(ctx, t, tx, userID, accountB, secID, 1000, 2000)
	seedChainHolding(ctx, t, tx, userID, accountC, secID, 1000, 5000)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 means the item edge A→B and the account edge B→C were never composed, so the "+
			"oldest link in the chain was summed beside the newest")
}

// TestPortfolioRepair_MixedPointerChain_IsTransitivelyClosed is the repair half of item D. The
// whole chain family was missing on the repair side, and the repair is the path that reported
// $12,000.
func TestPortfolioRepair_MixedPointerChain_IsTransitivelyClosed(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "repair-mixedptr")
	_, chainKey := chainOf(ctx, t, tx, accountA)
	itemC := seedChainItem(ctx, t, tx, userID, "repair-mixedptr-item3")
	accountC := seedChainAccount(ctx, t, tx, userID, itemC, &chainKey, "repair-mixedptr-c")
	replaceAccount(ctx, t, tx, accountB, &accountC)

	for _, a := range []uuid.UUID{accountA, accountB, accountC} {
		backdateActiveIntervalStart(ctx, t, tx, a, 30)
	}

	day := dayUTC(ctx, t, tx, 3)
	seedHoldingSnapshot(ctx, t, tx, userID, accountA, secID, day, true, f64(1000), f64(4000))
	seedHoldingSnapshot(ctx, t, tx, userID, accountB, secID, day, true, f64(1000), f64(2000))
	seedHoldingSnapshot(ctx, t, tx, userID, accountC, secID, day, true, f64(1000), f64(5000))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1, 1, 0, nil)

	sum, err := NewPortfolioSnapshotRepository(tx).RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	require.Equal(t, 1, sum.Repaired)

	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 5000.0, got.totalValue, 0.001,
		"11000 is the mixed-pointer double count rebuilt into recorded history, where it is permanent")
}

// TestPortfolioRepair_ThreeLinkChainOnOneDay_ReconstructsOnce is the repair half of the transitive
// walk. The Upsert side has had this test since round 9; the repair side had only two chain tests
// at all.
func TestPortfolioRepair_ThreeLinkChainOnOneDay_ReconstructsOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "repair-3link")
	itemB, chainKey := chainOf(ctx, t, tx, accountB)
	itemC := seedChainItem(ctx, t, tx, userID, "repair-3link-item3")
	accountC := seedChainAccount(ctx, t, tx, userID, itemC, &chainKey, "repair-3link-c")
	_, err = tx.Exec(ctx,
		`UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
		itemB, itemC)
	require.NoError(t, err)
	replaceAccount(ctx, t, tx, accountB, nil)

	for _, a := range []uuid.UUID{accountA, accountB, accountC} {
		backdateActiveIntervalStart(ctx, t, tx, a, 30)
	}

	day := dayUTC(ctx, t, tx, 3)
	// B, the middle link, was never captured on the day — the shape that let A survive.
	seedHoldingSnapshot(ctx, t, tx, userID, accountA, secID, day, true, f64(1000), f64(4000))
	seedHoldingSnapshot(ctx, t, tx, userID, accountC, secID, day, true, f64(1000), f64(5000))
	seedPortfolioSnapshot(ctx, t, tx, userID, day, 1, 1, 0, nil)

	sum, err := NewPortfolioSnapshotRepository(tx).RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	require.Equal(t, 1, sum.Repaired)
	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, 5000.0, got.totalValue, 0.001,
		"9000 is the three-link double count written into recorded history")
}

// ── topology 5, with UNEQUAL predecessor values ───────────────────────────────────────────────

// TestPortfolioSnapshot_Upsert_TwoMasklessPredecessorsOneSuccessor_KeepsTheDETERMINISTICSurvivor
// re-runs topology 5 with values that DIFFER, so the assertion can tell which predecessor
// survived rather than only how many did. The rationing orders by account id and excludes the
// lowest-ranked, so the survivor is the HIGHER id — arbitrary, but pinned, and a change of
// tie-break is a change of the number the user sees.
func TestPortfolioSnapshot_Upsert_TwoMasklessPredecessorsOneSuccessor_KeepsTheDETERMINISTICSurvivor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, a1, successor, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-unequal")
	oldItem, chainKey := chainOf(ctx, t, tx, a1)
	a2 := seedChainAccount(ctx, t, tx, userID, oldItem, &chainKey, "psnap-unequal-ira")
	replaceAccount(ctx, t, tx, a2, nil)

	values := map[uuid.UUID]float64{a1: 1500, a2: 2500}
	for acct, v := range values {
		seedChainHolding(ctx, t, tx, userID, acct, secID, 500, v)
	}
	seedChainHolding(ctx, t, tx, userID, successor, secID, 500, 5000)

	// The excluded predecessor is the lower id; the survivor is the higher one.
	survivor := a2
	if a1.String() > a2.String() {
		survivor = a1
	}
	want := 5000 + values[survivor]

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, want, snap.TotalValue, 0.001,
		"with distinct values the total names WHICH predecessor survived; equal values could not. "+
			"9000 means neither was excluded, 5000 means one successor rationed away both")
}

// ── topology 6: the successor item reports NEITHER sibling ────────────────────────────────────

// TestPortfolioSnapshot_Upsert_SuccessorReportsNeitherSibling_KeepsBoth is topology 6. The re-link
// happened, but the new item's roster contains no account matching that chain key at all — the
// successor row for the chain simply does not exist. Nothing observed anything, so nothing may be
// rationed away: both retained predecessors are the only record of the user's money that day.
func TestPortfolioSnapshot_Upsert_SuccessorReportsNeitherSibling_KeepsBoth(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, a1, successor, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-neither")
	oldItem, chainKey := chainOf(ctx, t, tx, a1)
	a2 := seedChainAccount(ctx, t, tx, userID, oldItem, &chainKey, "psnap-neither-ira")
	replaceAccount(ctx, t, tx, a2, nil)

	// The successor the helper created belongs to a DIFFERENT logical account: the new roster
	// carried the chain's siblings nowhere.
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = NULL WHERE id = $1`, successor)
	require.NoError(t, err)

	seedChainHolding(ctx, t, tx, userID, a1, secID, 500, 1500)
	seedChainHolding(ctx, t, tx, userID, a2, secID, 500, 2500)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 4000.0, snap.TotalValue, 0.001,
		"anything less means an account with no observed successor at all was rationed away and "+
			"the user's retained money vanished from the day")
}

// ── topology 8: the predecessor's item is ITSELF already a successor ──────────────────────────

// TestPortfolioSnapshot_Upsert_PredecessorItemIsItselfASuccessor_CountsOnce is topology 8. The
// chain has a re-link in its PAST (item0 → item1, long dead) as well as one today (item1 →
// item2). The closure is seeded from items the user has live rows under, so an inbound edge from a
// dead item must neither be walked into the live set nor disturb the rationing of the live one.
func TestPortfolioSnapshot_Upsert_PredecessorItemIsItselfASuccessor_CountsOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-topo8")
	oldItem, chainKey := chainOf(ctx, t, tx, accountA)

	// An ancestor item, replaced by the old one long ago, whose account is NOT live today.
	ancestorItem := seedChainItem(ctx, t, tx, userID, "psnap-topo8-item0")
	ancestor := seedChainAccount(ctx, t, tx, userID, ancestorItem, &chainKey, "psnap-topo8-ancestor")
	replaceAccount(ctx, t, tx, ancestor, nil)
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = NOW() - INTERVAL '90 days',
		       valid_to = NOW() - INTERVAL '60 days'
		WHERE account_id = $1`, ancestor)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE linked_items SET status = 'replaced', replaced_at = NOW() - INTERVAL '60 days',
		       replaced_by_id = $2 WHERE id = $1`, ancestorItem, oldItem)
	require.NoError(t, err)

	seedChainHolding(ctx, t, tx, userID, ancestor, secID, 500, 9999)
	seedChainHolding(ctx, t, tx, userID, accountA, secID, 1000, 4000)
	seedChainHolding(ctx, t, tx, userID, accountB, secID, 1000, 5000)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"the long-dead ancestor is not live today and contributes nothing; 9000 means the live "+
			"predecessor escaped exclusion once its own item had an inbound edge")
}

// ── topology 11: a 'replaced' account that is not part of any chain ───────────────────────────

// TestPortfolioSnapshot_Upsert_ReplacedAccountOutsideAnyChain_StillCounts is topology 11. Status
// alone must never decide a past day: an account marked 'replaced' with no chain key, no item
// pointer and no successor anywhere is still the only record of that money, and it was live.
func TestPortfolioSnapshot_Upsert_ReplacedAccountOutsideAnyChain_StillCounts(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "psnap-orphanreplaced")
	replaceAccount(ctx, t, tx, accountID, nil)
	seedChainHolding(ctx, t, tx, userID, accountID, secID, 1000, 3300)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 3300.0, snap.TotalValue, 0.001,
		"0 means the exclusion was reading accounts.status rather than a recorded succession")
}

// ── topology 10: a provider_absent account restored the same day ──────────────────────────────

// TestPortfolioSnapshot_Upsert_ProviderAbsentAccountRestoredSameDay_CountsOnce is topology 10.
// RestoreProviderRemovedAccount un-removes a row the bank stopped reporting, which writes a SECOND
// 'active' interval for the same account on the same day. Two overlapping live intervals for ONE
// account must not make it count twice.
func TestPortfolioSnapshot_Upsert_ProviderAbsentAccountRestoredSameDay_CountsOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "psnap-restored")
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = 'provider_absent'
		WHERE id = $1`, accountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'active', removed_at = NULL, removed_reason = NULL WHERE id = $1`, accountID)
	require.NoError(t, err)

	var intervals int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM account_status_history WHERE account_id = $1 AND status = 'active'`,
		accountID).Scan(&intervals))
	require.Equal(t, 2, intervals, "the fixture must really produce two 'active' intervals on one day")

	seedChainHolding(ctx, t, tx, userID, accountID, secID, 1000, 2750)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 2750.0, snap.TotalValue, 0.001,
		"5500 means the account joined its own liveness rows twice and its holdings were doubled")
}

// ── item F: chains longer than the old 16-level recursion cap ─────────────────────────────────

// TestPortfolioSnapshot_Upsert_ChainLongerThanTheOldRecursionCap_StillCountsOnce is item F. The
// walk was depth-capped at 16 with `depth` inside the recursive tuple, which both bounded the
// closure and weakened the UNION dedup it relied on. A chain with more edges than the cap let the
// first and last member both count. Nothing anywhere proves a chain cannot exceed 16 links, so the
// cap is removed rather than raised: over a FINITE set of accounts, a UNION closure that does not
// carry depth converges on its own and terminates on a cycle.
func TestPortfolioSnapshot_Upsert_ChainLongerThanTheOldRecursionCap_StillCountsOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, head, second, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-deep")
	prevItem, chainKey := chainOf(ctx, t, tx, second)
	prevAccount := second

	// Extend to 20 edges in total. Every intermediate is unobserved, so only the head and the tail
	// carry holdings — exactly the shape in which a truncated walk lets both count.
	const extraLinks = 19
	var tail uuid.UUID
	for i := 0; i < extraLinks; i++ {
		item := seedChainItem(ctx, t, tx, userID, "psnap-deep-item")
		acct := seedChainAccount(ctx, t, tx, userID, item, &chainKey, "psnap-deep-acct")
		_, err = tx.Exec(ctx,
			`UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
			prevItem, item)
		require.NoError(t, err)
		replaceAccount(ctx, t, tx, prevAccount, nil)
		prevItem, prevAccount = item, acct
		tail = acct
	}

	seedChainHolding(ctx, t, tx, userID, head, secID, 1000, 4000)
	seedChainHolding(ctx, t, tx, userID, tail, secID, 1000, 5000)

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
	require.NoError(t, err)
	assert.InDelta(t, 5000.0, snap.TotalValue, 0.001,
		"9000 means the walk ran out of depth before it reached the end of the chain, so the "+
			"oldest link was summed beside the newest")
}

// TestPortfolioSnapshot_Upsert_CyclicSuccessionPointer_TerminatesAndCountsEverything proves the
// cap's removal did not trade a bounded wrong answer for a query that never returns. A cyclic
// pointer is corrupt data; the requirement is that the query TERMINATES and that no account is
// silently deleted from the user's total.
func TestPortfolioSnapshot_Upsert_CyclicSuccessionPointer_TerminatesAndCountsEverything(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-cycle")
	itemA, _ := chainOf(ctx, t, tx, accountA)
	itemB, _ := chainOf(ctx, t, tx, accountB)
	// B's item points back at A's: a two-node cycle.
	_, err = tx.Exec(ctx,
		`UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
		itemB, itemA)
	require.NoError(t, err)

	seedChainHolding(ctx, t, tx, userID, accountA, secID, 1000, 4000)
	seedChainHolding(ctx, t, tx, userID, accountB, secID, 1000, 5000)

	done := make(chan struct{})
	var total float64
	go func() {
		defer close(done)
		snap, e := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
		require.NoError(t, e)
		total = snap.TotalValue
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the succession walk did not terminate on a cyclic pointer")
	}
	assert.InDelta(t, 9000.0, total, 0.001,
		"with each account superseding the other, neither is a survivor and nothing may be "+
			"rationed away — corrupt pointers must not delete a user's money")
}

// TestPortfolioSnapshot_Upsert_CycleWithAnOutgoingTail_StillRationsNobody is #1569 T10-FIX item 4.
//
// The cycle-safety property was stated unconditionally — "under a cycle nobody is a survivor and
// nothing is rationed away" — while holding for exactly one topology, the closed two-node cycle the
// test above covers. Add ONE real edge leaving the cycle and it stops holding: for A → B, B → A,
// B → C the closure derives A → C as well, C supersedes nobody, so (A,C) and (B,C) are both
// surviving pairs and the rationing retires a member of the cycle. Its money disappears.
//
// A corrupt pointer must not delete a user's money. That principle is not topology-specific, and it
// is what this fixture holds the implementation to.
func TestPortfolioSnapshot_Upsert_CycleWithAnOutgoingTail_StillRationsNobody(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// A → B by the item pointer, from the shared helper.
	userID, accountA, accountB, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-cycletail")
	itemA, chainKey := chainOf(ctx, t, tx, accountA)
	itemB, _ := chainOf(ctx, t, tx, accountB)

	// B → A closes the cycle.
	_, err = tx.Exec(ctx,
		`UPDATE linked_items SET status = 'replaced', replaced_at = NOW(), replaced_by_id = $2 WHERE id = $1`,
		itemB, itemA)
	require.NoError(t, err)

	// B → C is the outgoing tail, recorded by the ACCOUNT pointer because B's item pointer is
	// already spoken for by the cycle. The status is deliberately left 'active': this test is about
	// what the succession walk does, and changing B's liveness would answer a different question.
	itemC := seedChainItem(ctx, t, tx, userID, "psnap-cycletail-item3")
	accountC := seedChainAccount(ctx, t, tx, userID, itemC, &chainKey, "psnap-cycletail-c")
	_, err = tx.Exec(ctx, `UPDATE accounts SET replaced_by_id = $2 WHERE id = $1`, accountB, accountC)
	require.NoError(t, err)

	seedChainHolding(ctx, t, tx, userID, accountA, secID, 1000, 4000)
	seedChainHolding(ctx, t, tx, userID, accountB, secID, 1000, 5000)
	seedChainHolding(ctx, t, tx, userID, accountC, secID, 1000, 2000)

	done := make(chan struct{})
	var total float64
	go func() {
		defer close(done)
		snap, e := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), todayUTCFromDB(ctx, t, tx))
		require.NoError(t, e)
		total = snap.TotalValue
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the succession walk did not terminate on a cyclic pointer with a tail")
	}
	assert.InDelta(t, 11000.0, total, 0.001,
		"7000 is A rationed away by the A→C edge the cycle manufactured; 6000 is both cycle "+
			"members gone. A pointer that says two accounts replaced each other is corrupt, and "+
			"corrupt data must not delete money — however many real edges leave the cycle")
}

// ── item C: the two callers' $OBSERVED arms, MEASURED ─────────────────────────────────────────

// TestPortfolioSnapshot_ObservedArmsDisagree_TheRepairRefusesRatherThanRewrites is the measurement
// that settled item C, where two reviewers reached opposite conclusions from source alone.
//
// The arms genuinely differ: the Upsert asks "does the successor have ANY current holdings row"
// (no date predicate at all), the repair asks "does it have holding_snapshots for THIS day". The
// shape that separates them is a successor whose last sync predates the day being valued — a stale
// `holdings` row counts as evidence for a day it says nothing about. Reviewer 1 held that this
// makes a wrong value permanent; reviewer 2 held that the arms differ intentionally.
//
// MEASURED, and the numbers are asserted below: the Upsert excludes the predecessor and the repair
// keeps it, exactly as claimed — but keeping it makes the day PARTIALLY COVERED, and partial
// coverage is refused, not rebuilt. The divergence therefore never writes a number; it produces a
// refusal the ledger records. Nothing is made permanently wrong, so the arms are left as they are
// and the false "same string, so they agree" claim in repair.go is corrected instead.
func TestPortfolioSnapshot_ObservedArmsDisagree_TheRepairRefusesRatherThanRewrites(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, pred, succ, secID := seedRelinkedInvestmentChain(ctx, t, tx, "psnap-armsdiverge")
	backdateActiveIntervalStart(ctx, t, tx, pred, 30)
	backdateActiveIntervalStart(ctx, t, tx, succ, 30)

	day := dayUTC(ctx, t, tx, 3)

	// The predecessor: retained holdings AND a captured row for the day.
	seedChainHolding(ctx, t, tx, userID, pred, secID, 3000, 7000)
	seedHoldingSnapshot(ctx, t, tx, userID, pred, secID, day, true, f64(3000), f64(7000))

	// The successor: a CURRENT holdings row (it synced at some point), but the day in question was
	// skipped by a partial roster — no holding_snapshots row and no observation marker for it.
	seedChainHolding(ctx, t, tx, userID, succ, secID, 3000, 5000)

	// Arm 1 (Upsert): the date-free `holdings` test sees the successor and drops the predecessor.
	upserted, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	assert.InDelta(t, 5000.0, upserted.TotalValue, 0.001,
		"MEASURED: the Upsert's date-free arm counts a stale holdings row as evidence for this day")

	// Arm 2 (repair): the day-scoped test does NOT see the successor, so the predecessor stays in
	// the live set — and the successor, which bears holdings, then has no rows for the day.
	sum, err := NewPortfolioSnapshotRepository(tx).RepairUserFromHistory(ctx, userID.String())
	require.NoError(t, err)
	require.Equal(t, 1, sum.Examined)
	require.Equal(t, 0, sum.Repaired,
		"MEASURED: the divergence does not produce a rewritten number")

	l, ok := readLedger(ctx, t, tx, userID, day)
	require.True(t, ok)
	assert.Equal(t, "refused_partial_coverage", l.outcome,
		"MEASURED: keeping the predecessor makes the day two-account, one-covered, and partial "+
			"coverage is refused. The divergence is legible in the ledger, never silent")
	assert.Equal(t, 2, l.expectedCount)
	assert.Equal(t, 1, l.observedCount)

	got := readPortfolioSnapshot(ctx, t, tx, userID, day)
	assert.InDelta(t, upserted.TotalValue, got.totalValue, 0.001,
		"the refusal leaves the Upsert's value in place; the repair never overwrites it with a "+
			"reconstruction built from the sibling the Upsert had already excluded")
}
