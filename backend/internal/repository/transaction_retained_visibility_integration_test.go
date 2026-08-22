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

// disconnect soft-removes the whole chain, the way Task 2's DisconnectInstitution does. The
// account_status_history transition is written by the trigger.
func disconnect(ctx context.Context, t *testing.T, db DB, f retainedFixture) {
	t.Helper()
	_, err := db.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = NOW(), updated_at = NOW() WHERE id = $1`,
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

	before, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	require.Len(t, before, 2, "sanity: both rows are visible while the chain is active")

	disconnect(ctx, t, tx, f)

	after, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
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
	items, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
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

	got, _, err := repo.GetByUserIDKeysetUnified(ctx, mine.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"My Banking Row", "My Investment Row"}, feedNames(got),
		"the historical leg must not widen tenancy: the other user's retained rows stay out")

	gotTheirs, _, err := repo.GetByUserIDKeysetUnified(ctx, theirs.UserID.String(), domain.TransactionFilters{}, nil, 50)
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
	items, _, err := repo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"Orphaned Banking Row"}, feedNames(items),
		"truly orphaned rows have no account to prove anything about; they belong to the user and "+
			"must remain visible")
}
