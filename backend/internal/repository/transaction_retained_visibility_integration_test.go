//go:build integration

package repository

// Issue #1569 — retained ≠ visible.
//
// Task 2 stopped DELETING disconnected accounts; they are now soft-removed and kept. But the
// unified activity feed showed a transaction only if its account was 'active', or an 'active'
// account shared its user_account_id. A fully disconnected chain therefore has its transactions
// retained on disk and gone from the UI — which fails #1569's requirement exactly as completely
// as deleting them would have.
//
// The fix ADDS a historical leg beside the active-chain one, keyed on recorded liveness in
// account_status_history. It does NOT remove the active-chain leg: #1561 removed status
// predicates wholesale elsewhere and had to be reverted (balance_snapshot_repository.go narrates
// both failure modes).
//
// The negative test in this file is the one that matters most: a leg that reads a new table must
// not become a way for one user's transactions to appear in another user's feed.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// retainedFixture is one user with one linked item, one account and one user_account.
type retainedFixture struct {
	UserID        uuid.UUID
	LinkedItemID  uuid.UUID
	AccountID     uuid.UUID
	UserAccountID uuid.UUID
}

// seedRetainedFixture seeds an ACTIVE account plus its user_account. The account_status_history
// row is written by the trigger from migration 000152 — this helper deliberately does not write
// one by hand, so the tests exercise the same rows production will have.
func seedRetainedFixture(ctx context.Context, t *testing.T, db DB, prefix string) retainedFixture {
	t.Helper()
	f := retainedFixture{
		UserID:        uuid.New(),
		LinkedItemID:  uuid.New(),
		AccountID:     uuid.New(),
		UserAccountID: uuid.New(),
	}

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.UserID, "test|"+prefix+"-"+f.UserID.String(), prefix+"-"+f.UserID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, f.LinkedItemID, f.UserID, prefix+"-item-"+f.LinkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')
	`, f.UserAccountID, f.UserID, prefix+"-inst-"+f.UserAccountID.String())
	require.NoError(t, err, "seed user_account")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Bank Checking', 'depository', 'active', $5)
	`, f.AccountID, f.LinkedItemID, f.UserID, prefix+"-acct-"+f.AccountID.String(), f.UserAccountID)
	require.NoError(t, err, "seed account")

	return f
}

// seedBankingTxn inserts one banking transaction for the fixture on the given date.
func seedBankingTxn(ctx context.Context, t *testing.T, db DB, f retainedFixture, date time.Time, name string) uuid.UUID {
	t.Helper()
	txnID := uuid.New()
	ext := txnID.String()
	_, err := db.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, user_account_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary, category_source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7, 42.00, $8, $9, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, txnID, f.UserID, f.AccountID, f.LinkedItemID, f.UserAccountID, ext, ext, date, name)
	require.NoError(t, err, "seed transaction")
	return txnID
}

// seedInvestmentTxn inserts one investment transaction for the fixture on the given date.
func seedInvestmentTxn(ctx context.Context, t *testing.T, db DB, f retainedFixture, date time.Time, name string) uuid.UUID {
	t.Helper()
	txnID := uuid.New()
	ext := txnID.String()
	_, err := db.Exec(ctx, `
		INSERT INTO investment_transactions (id, user_id, account_id, user_account_id,
			plaid_investment_transaction_id, amount, date, name, type, quantity, price, fees, created_at)
		VALUES ($1, $2, $3, $4, $5, 100.00, $6, $7, 'buy', 1, 100.00, 0, NOW())
	`, txnID, f.UserID, f.AccountID, f.UserAccountID, ext, date, name)
	require.NoError(t, err, "seed investment transaction")
	return txnID
}

// seedOrphanBankingTxn inserts a banking transaction with NO user_account_id — the #1109 shape a
// pre-relink row has when the chain key was never backfilled onto it. It cannot be resolved by the
// active-chain leg, which is exactly what makes it a probe for this one.
func seedOrphanBankingTxn(ctx context.Context, t *testing.T, db DB, f retainedFixture, date time.Time, name string) uuid.UUID {
	t.Helper()
	txnID := uuid.New()
	ext := txnID.String()
	_, err := db.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, user_account_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary, category_source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULL, 'plaid', $5, $6, 7.00, $7, $8, false, 'FOOD_AND_DRINK', 'plaid', NOW(), NOW())
	`, txnID, f.UserID, f.AccountID, f.LinkedItemID, ext, ext, date, name)
	require.NoError(t, err, "seed orphan transaction")
	return txnID
}

// disconnect soft-removes the fixture's account the way DisconnectInstitution's write does:
// status, removed_at AND removed_reason in one statement, because production never leaves the
// cause unstamped (accountRepository.MarkRemovedByLinkedItemID, and migration 000154's
// accounts_removed_reason_valid CHECK, which only tolerates a NULL cause on a legacy row).
// A fixture that omitted it described a row production cannot write, on the exact column that
// decides whether a sync may resurrect the account (RestoreProviderRemovedAccount refuses
// anything but 'provider_absent').
//
// It is NOT the whole of DisconnectInstitution and does not claim to be: that call also removes
// the Plaid item and marks linked_items disconnected, and it is scoped by linked_item_id rather
// than by one account id. This is the accounts write, which is the only part these reads see.
// The account_status_history transition is written by the trigger.
func disconnect(ctx context.Context, t *testing.T, db DB, f retainedFixture) {
	t.Helper()
	_, err := db.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = 'user_disconnect', updated_at = NOW() WHERE id = $1`,
		f.AccountID)
	require.NoError(t, err, "soft-remove account")
}

func feedNames(items []*domain.ActivityItem) []string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	return names
}

// TestUnifiedFeed_DisconnectedChain_TransactionsStayVisible is the core Change D acceptance test.
// Both a banking and an investment transaction must survive the whole chain going 'removed'.
func TestUnifiedFeed_DisconnectedChain_TransactionsStayVisible(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-vis")
	txnDate := time.Now().UTC().AddDate(0, 0, -10)
	seedBankingTxn(ctx, t, tx, f, txnDate, "Coffee Before Disconnect")
	seedInvestmentTxn(ctx, t, tx, f, txnDate, "Buy Before Disconnect")

	repo := NewTransactionRepository(tx, nil)

	before, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	require.Len(t, before, 2, "sanity: both rows are visible while the chain is active")

	disconnect(ctx, t, tx, f)

	after, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Coffee Before Disconnect", "Buy Before Disconnect"}, feedNames(after),
		"a soft-removed chain's transactions are RETAINED; retained but invisible fails #1569 as "+
			"completely as deleting them")
}

// TestUnifiedFeed_DisconnectedChain_KeepsTransactionsDatedBeforeAccountCreation is the case a
// naive `h.valid_from <= t.date` lower bound silently destroys.
//
// account_status_history.valid_from for an account's first 'active' interval is accounts.created_at
// (migration 000152's backfill, and the INSERT trigger's clock_timestamp()). A Plaid initial sync
// pulls up to 24 MONTHS of transactions dated long before that row existed, so requiring the
// account to have been live on the transaction's own date would hide the overwhelming majority of
// a disconnected account's history — precisely the history #1569 exists to keep. The historical
// leg therefore bounds only the UPPER end of the interval.
func TestUnifiedFeed_DisconnectedChain_KeepsTransactionsDatedBeforeAccountCreation(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-backfill")
	// Two years before the account row was created — a normal Plaid backfill row.
	seedBankingTxn(ctx, t, tx, f, time.Now().UTC().AddDate(-2, 0, 0), "Ancient Backfilled Purchase")
	seedInvestmentTxn(ctx, t, tx, f, time.Now().UTC().AddDate(-2, 0, 0), "Ancient Backfilled Buy")

	disconnect(ctx, t, tx, f)

	repo := NewTransactionRepository(tx, nil)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"Ancient Backfilled Purchase", "Ancient Backfilled Buy"}, feedNames(items),
		"transactions dated before the account row existed are the bulk of an initial Plaid sync; "+
			"a lower bound on the historical leg would hide all of them once the chain is removed")
}

// TestUnifiedFeed_DisconnectedChain_DoesNotLeakAcrossUsers is the negative half. Two users each
// own a fully disconnected chain; neither feed may show the other's rows. The historical leg keys
// on t.account_id and adds no user predicate of its own, so this test is what proves the outer
// `t.user_id = $1` scoping still carries the whole tenancy guarantee.
func TestUnifiedFeed_DisconnectedChain_DoesNotLeakAcrossUsers(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	mine := seedRetainedFixture(ctx, t, tx, "ret-mine")
	theirs := seedRetainedFixture(ctx, t, tx, "ret-theirs")
	d := time.Now().UTC().AddDate(0, 0, -5)
	seedBankingTxn(ctx, t, tx, mine, d, "My Banking Row")
	seedInvestmentTxn(ctx, t, tx, mine, d, "My Investment Row")
	seedBankingTxn(ctx, t, tx, theirs, d, "Their Banking Row")
	seedInvestmentTxn(ctx, t, tx, theirs, d, "Their Investment Row")

	disconnect(ctx, t, tx, mine)
	disconnect(ctx, t, tx, theirs)

	repo := NewTransactionRepository(tx, nil)

	got, _, _, err := repo.GetByUserIDKeysetUnified(ctx, mine.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"My Banking Row", "My Investment Row"}, feedNames(got),
		"the historical leg must not widen tenancy: the other user's retained rows stay out")

	gotTheirs, _, _, err := repo.GetByUserIDKeysetUnified(ctx, theirs.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Their Banking Row", "Their Investment Row"}, feedNames(gotTheirs),
		"and symmetrically in the other direction")
}

// TestUnifiedFeed_OrphanedTransactions_StayVisible covers t.account_id IS NULL — what migration
// 000035's ON DELETE SET NULL produced for accounts hard-deleted before Task 2. Those rows match
// neither the active leg (NULL never satisfies `id = t.account_id`) nor the historical one (no
// account row to have history), so without an explicit disjunct they are retained and permanently
// invisible.
//
// Banking only, and that is a schema fact rather than an omission: investment_transactions.account_id
// is NOT NULL, so 000035's SET NULL never applied to it and an orphaned investment row cannot
// exist. The investment branch therefore gets no IS NULL disjunct.
func TestUnifiedFeed_OrphanedTransactions_StayVisible(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-orphan")
	d := time.Now().UTC().AddDate(0, 0, -3)
	bankTxn := seedBankingTxn(ctx, t, tx, f, d, "Orphaned Banking Row")

	// Simulate the legacy hard-delete aftermath directly: no account_id, and no chain to fall
	// back on either (a surviving user_account_id would be resolved by the active-chain leg).
	_, err = tx.Exec(ctx, `UPDATE transactions SET account_id = NULL, user_account_id = NULL WHERE id = $1`, bankTxn)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, nil)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"Orphaned Banking Row"}, feedNames(items),
		"truly orphaned rows have no account to prove anything about; they belong to the user and "+
			"must remain visible")
}

// ─── the two conditions that decide what this leg HIDES ──────────────────────────────────────
//
// Fix round 1: the four tests above all seed transactions the leg must SHOW, so deleting either
// half of the predicate that restricts it left them green. A leg whose restrictions are untested
// is a leg with no restrictions. The three tests below cover both axes — the interval's upper
// bound, and the no-live-successor scoping — and each was mutation-checked by deleting the
// conjunct it targets.

// closeActiveIntervalAt rewrites the account's (already closed) 'active' interval so it ended at
// `at`, and starts the terminal interval there. Done by hand because a test cannot wait 100 days
// for the trigger to produce a genuinely old transition.
func closeActiveIntervalAt(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID, at time.Time) {
	t.Helper()
	ct, err := db.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, accountID, at)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one active interval must have been closed")

	_, err = db.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND valid_to IS NULL`, accountID, at)
	require.NoError(t, err)
}

// TestUnifiedFeed_TransactionDatedAfterTheIntervalClosed_StaysHidden falsifies the interval's
// UPPER bound — the only condition in this leg that hides anything.
//
// A row dated after the account stopped being live is not history the account can vouch for: it
// is a stale write, a mis-dated import, or a row belonging to a period the account did not cover.
// The leg admits history, not everything that ever named the account. The control row, dated
// inside the interval, is what makes the assertion about the BOUND rather than about the account.
func TestUnifiedFeed_TransactionDatedAfterTheIntervalClosed_StaysHidden(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-upper")
	now := time.Now().UTC()
	seedBankingTxn(ctx, t, tx, f, now.AddDate(0, 0, -200), "Inside The Interval")
	seedBankingTxn(ctx, t, tx, f, now.AddDate(0, 0, -10), "After The Interval Closed")
	seedInvestmentTxn(ctx, t, tx, f, now.AddDate(0, 0, -200), "Inside The Interval Buy")
	seedInvestmentTxn(ctx, t, tx, f, now.AddDate(0, 0, -10), "After The Interval Closed Buy")

	disconnect(ctx, t, tx, f)
	// The account was live from a year ago until 100 days ago.
	_, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'active'`, f.AccountID, now.AddDate(0, 0, -365))
	require.NoError(t, err)
	closeActiveIntervalAt(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -100))

	repo := NewTransactionRepository(tx, nil)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Inside The Interval", "Inside The Interval Buy"}, feedNames(items),
		"only rows dated at or before the interval's close are the account's history; a row dated "+
			"after it must stay hidden, on BOTH branches")
}

// TestUnifiedFeed_FullyDisconnectedRelinkChain_ShowsPreRelinkTransactions is the case a
// status-list scoping ('removed','expired') silently lost, and the reason this leg keys on
// "no live successor" instead.
//
// Link bank A, re-link it (A -> 'replaced', B -> 'active' on the same user_account_id), then
// disconnect entirely (B -> 'removed'). B's rows come back through the leg. Under a status list,
// A's PRE-RELINK rows did not: the active leg has nothing active left to find, and 'replaced' was
// excluded. Every user who ever re-linked would have lost the older half of their history — the
// #1569 bug displaced one chain hop back.
func TestUnifiedFeed_FullyDisconnectedRelinkChain_ShowsPreRelinkTransactions(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	old := seedRetainedFixture(ctx, t, tx, "ret-relink-dead")
	now := time.Now().UTC()
	seedBankingTxn(ctx, t, tx, old, now.AddDate(0, 0, -60), "Pre-Relink Chained")

	// A pre-relink row whose user_account_id was never backfilled — the #1109 orphan shape. Once
	// the chain is fully dead there is no active sibling to resolve it either, so this leg is the
	// only thing that can keep it visible.
	seedOrphanBankingTxn(ctx, t, tx, old, now.AddDate(0, 0, -59), "Pre-Relink Unchained")

	// The re-link: a new active account on the SAME chain key, old one marked replaced.
	newAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Checking', 'depository', 'active', $5)`,
		newAccountID, old.LinkedItemID, old.UserID, "relink-"+newAccountID.String(), old.UserAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, old.AccountID)
	require.NoError(t, err)

	post := old
	post.AccountID = newAccountID
	seedBankingTxn(ctx, t, tx, post, now.AddDate(0, 0, -10), "Post-Relink")

	repo := NewTransactionRepository(tx, nil)
	live, _, _, err := repo.GetByUserIDKeysetUnified(ctx, old.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Pre-Relink Chained", "Post-Relink"}, feedNames(live),
		"sanity, and the #1109 contract: while the chain is live the unchained pre-relink row stays "+
			"hidden and the chained one is resolved by the active sibling")

	// Now the user disconnects the re-linked bank entirely — removed_reason stamped in the same
	// statement, as MarkRemovedByLinkedItemID does it.
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = 'user_disconnect', updated_at = NOW() WHERE id = $1`,
		newAccountID)
	require.NoError(t, err)

	dead, _, _, err := repo.GetByUserIDKeysetUnified(ctx, old.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"Pre-Relink Chained", "Pre-Relink Unchained", "Post-Relink"}, feedNames(dead),
		"with no active member left anywhere in the chain, EVERY retained row it owns must be "+
			"visible — including the pre-relink ones on the 'replaced' account")
}

// TestUnifiedFeed_LiveRelinkChain_KeepsUnchainedPreRelinkRowHidden falsifies the no-live-successor
// scoping itself. It is the same fixture as the test above, stopped one step earlier: the chain
// still has an active member, so the leg must stay closed and #1109's decision must stand.
//
// Deleting the NOT EXISTS conjunct makes this test RED. It duplicates
// TestTransactions_UnifiedFeed_BankingSurvivesRelink_Integration's orphan sub-case deliberately —
// that test lives in another file and covers many things at once, and the condition it happens to
// pin here is the one this predicate turns on.
func TestUnifiedFeed_LiveRelinkChain_KeepsUnchainedPreRelinkRowHidden(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	old := seedRetainedFixture(ctx, t, tx, "ret-relink-live")
	now := time.Now().UTC()
	seedOrphanBankingTxn(ctx, t, tx, old, now.AddDate(0, 0, -59), "Unchained Pre-Relink Row")

	newAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Checking', 'depository', 'active', $5)`,
		newAccountID, old.LinkedItemID, old.UserID, "relink-live-"+newAccountID.String(), old.UserAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = NOW() WHERE id = $1`, old.AccountID)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, nil)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, old.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.Empty(t, feedNames(items),
		"an active sibling still owns this chain, so #1109's rule holds and the historical leg must "+
			"not fire. Widening it here would silently overturn a decision this issue does not own.")
}

// TestUnifiedFeed_ExpiredChain_TransactionsStayVisible covers the OTHER terminal state.
// ExpireAccountsByFingerprint writes 'expired' (not 'removed') for a whole fingerprint that has
// been disconnected past its hold, and every earlier test in this file reaches the leg via
// 'removed' only. Under the shipped no-live-successor predicate this passes because an expired
// chain has no active member — which is the point: the leg is keyed on successorship, so it covers
// terminal states nobody enumerated.
func TestUnifiedFeed_ExpiredChain_TransactionsStayVisible(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-expired")
	now := time.Now().UTC()
	seedBankingTxn(ctx, t, tx, f, now.AddDate(0, 0, -120), "Expired Chain Purchase")
	seedInvestmentTxn(ctx, t, tx, f, now.AddDate(0, 0, -120), "Expired Chain Buy")

	// accounts_expired_at_consistency (migration 000155, Issue #1569 Task 6) requires expired_at
	// whenever status = 'expired'.
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'expired', expired_at = NOW(), updated_at = NOW() WHERE id = $1`, f.AccountID)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, nil)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Expired Chain Purchase", "Expired Chain Buy"}, feedNames(items),
		"a fingerprint-expired chain is as disconnected as a removed one and its history is retained "+
			"the same way")
}

// invTxnNames flattens the investments-page result the same way feedNames does for the feed.
func invTxnNames(txns []domain.InvestmentTransaction) []string {
	names := make([]string, 0, len(txns))
	for _, t := range txns {
		names = append(names, t.Name)
	}
	return names
}

// TestDisconnectedChain_InvestmentTransactions_VisibleOnBothSurfaces closes the divergence this
// branch itself created.
//
// The historical leg was added to BOTH branches of GetByUserIDKeysetUnified (the activity feed)
// but to neither read behind the investments page — investmentTransactionRepository.GetByUserIDKeyset
// (ListInvestmentTransactions) nor GetByUserID. Before Task 2 both surfaces agreed on "empty",
// because a disconnect CASCADE-deleted the rows; afterwards the same investment transaction is
// listed in the activity feed and missing from the investments page, which is a worse state than
// either surface alone. The two reads must answer the same question.
func TestDisconnectedChain_InvestmentTransactions_VisibleOnBothSurfaces(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "ret-inv-both")
	txnDate := time.Now().UTC().AddDate(0, 0, -10)
	seedInvestmentTxn(ctx, t, tx, f, txnDate, "Buy Before Disconnect")

	txnRepo := NewTransactionRepository(tx, nil)
	invRepo := NewInvestmentTransactionRepository(tx)

	keysetBefore, _, err := invRepo.GetByUserIDKeyset(ctx, f.UserID.String(),
		domain.InvestmentTransactionFilters{}, nil, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Buy Before Disconnect"}, invTxnNames(keysetBefore),
		"sanity: the investments page lists the row while the chain is active")

	disconnect(ctx, t, tx, f)

	feed, _, _, err := txnRepo.GetByUserIDKeysetUnified(ctx, f.UserID.String(),
		domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	require.Equal(t, []string{"Buy Before Disconnect"}, feedNames(feed),
		"sanity: the activity feed already retains it — that is the surface this branch fixed")

	keysetAfter, _, err := invRepo.GetByUserIDKeyset(ctx, f.UserID.String(),
		domain.InvestmentTransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.Equal(t, []string{"Buy Before Disconnect"}, invTxnNames(keysetAfter),
		"ListInvestmentTransactions backs the investments page — it must not disagree with the feed")

	offsetAfter, err := invRepo.GetByUserID(ctx, f.UserID.String(), 50, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"Buy Before Disconnect"}, invTxnNames(offsetAfter),
		"the offset-paginated read must agree with the keyset one; two answers to one question is "+
			"how this divergence appeared in the first place")
}

// TestDisconnectedChain_InvestmentTransactions_DoNotLeakAcrossUsers is the tenancy guard for the
// leg added above: it keys on it.account_id and adds no user predicate of its own, exactly like the
// feed's, so ownership rests entirely on the outer it.user_id = $1.
func TestDisconnectedChain_InvestmentTransactions_DoNotLeakAcrossUsers(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	victim := seedRetainedFixture(ctx, t, tx, "ret-inv-victim")
	other := seedRetainedFixture(ctx, t, tx, "ret-inv-other")
	txnDate := time.Now().UTC().AddDate(0, 0, -10)
	seedInvestmentTxn(ctx, t, tx, victim, txnDate, "Victim Buy")

	disconnect(ctx, t, tx, victim)

	invRepo := NewInvestmentTransactionRepository(tx)

	keyset, _, err := invRepo.GetByUserIDKeyset(ctx, other.UserID.String(),
		domain.InvestmentTransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.Empty(t, invTxnNames(keyset), "another user's disconnected-account rows must stay invisible")

	offset, err := invRepo.GetByUserID(ctx, other.UserID.String(), 50, 0)
	require.NoError(t, err)
	assert.Empty(t, invTxnNames(offset), "another user's disconnected-account rows must stay invisible")
}
