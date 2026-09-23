//go:build integration

package repository

// #1569 T12 (Half A) — the two retention universes, pinned behaviourally.
//
// Eleven tasks made this issue's answer "retain, do not delete". That answer is NOT
// "show everything everywhere", and this file is where the difference is written down as
// executable behaviour rather than as a comment somebody can drift away from.
//
// THE RULING (see account_liveness_sql.go, currentStateAccountLeg, for the reasoning at the
// code):
//
//	EVENT universe   — a dated thing that HAPPENED. True forever; a later disconnection cannot
//	                   un-happen it. Reads here are status-agnostic or carry
//	                   retainedAccountVisibilityLeg. Banking analytics, the unified feed, the
//	                   investment-transaction list, and (as of this task) IncomeSummary.
//
//	CURRENT-STATE universe — what is held / owed RIGHT NOW. `holdings` and the three liability
//	                   tables are replace-semantics: the row carries no as-of date, so surfacing
//	                   a terminal account's row states a stale position as a live one. Reads here
//	                   stay scoped to status = 'active'.
//
//	HISTORICAL-STATE universe — what was held / owed THEN. holding_snapshots /
//	                   liability_statements (tasks 6/10), which DO carry an as_of_date and are
//	                   deliberately independent of the account's current status.
//
// The defect task 12 was handed was not that holdings are hidden for a disconnected brokerage.
// It is that nobody had chosen it, and that one query (IncomeSummary) sat on the wrong side of a
// line nobody had drawn. Both are addressed here.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedRetentionUniverseFixture builds one brokerage with:
//   - a live holding worth 5000 (current-state universe)
//   - a holding_snapshots row for today (historical-state universe)
//   - a dividend investment_transaction dated inside the income window (event universe)
//
// Returns (userID, accountID, securityID, today).
func seedRetentionUniverseFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, accountID, secID uuid.UUID, today time.Time,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID := uuid.New()
	accountID = uuid.New()
	secID = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, current_balance)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'active', 0)
	`, accountID, linkedItemID, userID, prefix+"-acct-"+accountID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, type)
		VALUES ($1, $2, 'AAPL', 'stock')
	`, secID, prefix+"-sec-"+secID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 50, 4000, 5000)
	`, uuid.New(), userID, accountID, secID)
	require.NoError(t, err)

	// Historical-state universe: one capture, filed under the SERVER's UTC day.
	today, err = serverUTCDate(ctx, db)
	require.NoError(t, err)
	require.NoError(t, holdingsReplaced(captureHoldingSnapshots(ctx, db, accountID.String(), userID.String())))

	// Event universe: a dividend inside the income window. Plaid stores income as NEGATIVE.
	itxnID := uuid.New()
	_, err = db.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'dividend', 'AAPL dividend', -120.00, $6::date)
	`, itxnID, userID, accountID, secID, prefix+"-itxn-"+itxnID.String(), today)
	require.NoError(t, err)

	return userID, accountID, secID, today
}

// disconnectAccount drives the account to the terminal state a Plaid disconnect leaves behind.
// The account_status_history trigger closes the open 'active' interval as a side effect, which
// is what retainedAccountVisibilityLeg reads.
func disconnectAccount(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = now() WHERE id = $1
	`, accountID)
	require.NoError(t, err)
}

// TestRetentionUniverses_DisconnectedBrokerage_Integration is the single behavioural statement of
// the Half A ruling. Every assertion below is a decision, not an observation.
func TestRetentionUniverses_DisconnectedBrokerage_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountID, _, today := seedRetentionUniverseFixture(ctx, t, tx, "univ")

	holdingRepo := NewHoldingRepository(tx)
	snapRepo := NewHoldingSnapshotRepository(tx)
	analyticsRepo := NewInvestmentAnalyticsRepository(tx)
	itxnRepo := NewInvestmentTransactionRepository(tx)

	start := today.AddDate(0, 0, -1)
	end := today.AddDate(0, 0, 1)

	// ── While connected, all three universes agree. ───────────────────────────────────────
	live, err := holdingRepo.GetByUserID(ctx, userID.String())
	require.NoError(t, err)
	require.Len(t, live, 1, "connected: current-state universe has the position")

	incomeBefore, err := analyticsRepo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.InDelta(t, 120.0, incomeBefore.TotalIncome, 0.01, "connected: the dividend counts")

	// ── Disconnect. ───────────────────────────────────────────────────────────────────────
	disconnectAccount(ctx, t, tx, accountID)

	// CURRENT-STATE universe closes. A replace-semantics row has no as-of date; showing it now
	// would assert a live position that nobody has confirmed since the disconnection.
	afterLive, err := holdingRepo.GetByUserID(ctx, userID.String())
	require.NoError(t, err)
	assert.Empty(t, afterLive, "disconnected: GetByUserID must not report a stale position as live")

	afterSecurities, err := holdingRepo.GetByUserIDWithSecurities(ctx, userID.String(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, afterSecurities, "disconnected: GetByUserIDWithSecurities is current-state too")

	alloc, err := analyticsRepo.AllocationBreakdown(ctx, userID.String(), domain.AllocationOptions{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, alloc.TotalValue, 0.01,
		"disconnected: allocation is a current-composition question")

	// EVENT universe stays open. The dividend was received; a later disconnection did not
	// un-receive it. This is the leg the investment-transaction list already carried and that
	// IncomeSummary was missing.
	itxns, err := itxnRepo.GetByUserID(ctx, userID.String(), 50, 0)
	require.NoError(t, err)
	assert.Len(t, itxns, 1, "disconnected: the investment transaction is retained AND visible")

	incomeAfter, err := analyticsRepo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	assert.InDelta(t, 120.0, incomeAfter.TotalIncome, 0.01,
		"disconnected: income already received must not vanish from the year's total")
	assert.Equal(t, incomeBefore.TotalIncome, incomeAfter.TotalIncome,
		"disconnecting an account must not change a HISTORICAL income figure at all")

	// HISTORICAL-STATE universe stays open, and is where the position remains reachable. This
	// is what makes hiding it above a routing decision rather than a data loss.
	hist, err := snapRepo.GetByAccountDateRange(ctx, accountID.String(), start, end)
	require.NoError(t, err)
	require.Len(t, hist, 1, "disconnected: the last confirmed position is still on record")
	assert.InDelta(t, 5000.0, *hist[0].InstitutionValue, 0.01)
	assert.Equal(t, today.Format("2006-01-02"), hist[0].AsOfDate.Format("2006-01-02"),
		"the historical row carries the as-of date the current-state row lacks")
}

// TestRetentionUniverses_RelinkedBrokerage_IncomeSurvives_Integration is the COMPOSED case the
// test above cannot reach.
//
// TestRetentionUniverses_DisconnectedBrokerage_Integration disconnects and never re-links, so the
// brokerage's chain has no 'active' member left — which is exactly the case
// retainedAccountVisibilityLeg is scoped to. The complementary case is a re-link: the old account
// goes 'replaced' and a NEW account on the SAME user_account_id goes 'active'. The retained leg
// then deliberately does NOT fire (its NOT EXISTS finds the live successor), and visibility is
// supposed to come from the chain-sibling leg beside it — the one
// investmentTransactionRepository.GetByUserIDKeyset carries and IncomeSummary did not.
//
// The two legs are COMPLEMENTS, so omitting one leaves the whole re-link case uncovered: the
// investments feed listed the dividend while IncomeSummary returned 0.00 over the same rows. That
// is the same "two reads over the same rows disagree" defect task 12 fixed for the disconnect
// case, displaced one chain hop.
func TestRetentionUniverses_RelinkedBrokerage_IncomeSurvives_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, oldAccountID, _, today := seedRetentionUniverseFixture(ctx, t, tx, "relink")

	// Give the old account a chain key, and hang the dividend off it — this is what a Plaid sync
	// writes (transactions and investment_transactions carry user_account_id at insert time,
	// Issue #1109).
	userAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'investment')
	`, userAccountID, userID, "relink-inst-"+userAccountID.String())
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $2 WHERE id = $1`, oldAccountID, userAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE investment_transactions SET user_account_id = $2 WHERE account_id = $1`, oldAccountID, userAccountID)
	require.NoError(t, err)

	analyticsRepo := NewInvestmentAnalyticsRepository(tx)
	itxnRepo := NewInvestmentTransactionRepository(tx)

	start := today.AddDate(0, 0, -1)
	end := today.AddDate(0, 0, 1)

	before, err := analyticsRepo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.InDelta(t, 120.0, before.TotalIncome, 0.01, "pre-relink: the dividend counts")

	// ── Re-link: a NEW active account on the SAME chain replaces the old one. ─────────────
	var linkedItemID uuid.UUID
	require.NoError(t, tx.QueryRow(ctx, `SELECT linked_item_id FROM accounts WHERE id = $1`, oldAccountID).Scan(&linkedItemID))

	newAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status,
		                      current_balance, user_account_id)
		VALUES ($1, $2, $3, $4, 'New Brokerage', 'investment', 'active', 0, $5)
	`, newAccountID, linkedItemID, userID, "relink-acct-"+newAccountID.String(), userAccountID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_at = now(), replaced_by_id = $2 WHERE id = $1
	`, oldAccountID, newAccountID)
	require.NoError(t, err)

	// The feed over the SAME rows still shows the dividend — the chain-sibling leg fires.
	itxns, err := itxnRepo.GetByUserID(ctx, userID.String(), 50, 0)
	require.NoError(t, err)
	require.Len(t, itxns, 1, "re-linked: the investment-transaction feed keeps the pre-relink dividend")

	after, err := analyticsRepo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	assert.InDelta(t, 120.0, after.TotalIncome, 0.01,
		"re-linked: income already received must not vanish; the feed over the same rows still shows it")
	assert.Equal(t, before.TotalIncome, after.TotalIncome,
		"re-linking a brokerage must not change a HISTORICAL income figure at all")
}
