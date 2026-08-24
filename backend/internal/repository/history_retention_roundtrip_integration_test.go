//go:build integration

package repository

// Issue #1569 Task 19 — Guard G3: the round-trip acceptance test.
//
// G1 pins the schema and G2 pins the Go source, but both are STRUCTURAL: they would stay green if
// every FK and every DELETE were correct and the user still lost their history, because history can
// also be lost by a read that stops finding it, a dedup key that collapses two accounts into one, or
// a purge that takes more than it named. This test is the behavioural statement, end to end, of what
// #1569 promised:
//
//   1. Disconnect a bank              -> balance, transaction, holding and liability history survive.
//   2. Change institutions            -> last month is intact, and the overlap day is not double-counted.
//   3. Purge                          -> exactly the named data dies, and nothing else.
//
// It calls the PRODUCTION methods (MarkRemovedByLinkedItemID, UpdateStatus, GetAggregatedHistory,
// PurgeAccount) rather than re-stating their SQL, which is the only version of this test that a
// regression in those methods can fail. Several existing fixtures in this package inline the
// disconnect UPDATE for speed; this one deliberately does not.
//
// It runs in CI under the existing "Run integration tests" step in .github/workflows/test.yml
// (referenced by step name — the line number has gone stale three times), which supplies
// DATABASE_URL and passes -tags integration -p 1. No new job.
//
// Mutation-checked against real production code; see task-19-report.md for the three mutants and
// which assertion each one failed on.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedRoundTripHoldingSnapshot inserts one holding_snapshots row — the per-day investment position record
// created by #1569 task 13, which Plaid does not backfill.
func seedRoundTripHoldingSnapshot(ctx context.Context, t *testing.T, db DB, f retainedFixture, asOf time.Time) uuid.UUID {
	t.Helper()
	var securityID uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO securities (plaid_security_id, name) VALUES ($1, 'Roundtrip Security') RETURNING id
	`, "rt-sec-"+uuid.New().String()).Scan(&securityID), "seed security")

	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO holding_snapshots (user_id, account_id, user_account_id, security_id, as_of_date, quantity)
		VALUES ($1, $2, $3, $4, $5, 12)
		RETURNING id
	`, f.UserID, f.AccountID, f.UserAccountID, securityID, asOf).Scan(&id), "seed holding_snapshot")
	return id
}

// seedLiabilityStatement inserts one liability_statements row — the captured statement terms from
// #1569 task 13. The provider only ever reports the CURRENT statement, so this row is the only
// record that this one existed.
func seedLiabilityStatement(ctx context.Context, t *testing.T, db DB, f retainedFixture, asOf time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO liability_statements (user_id, account_id, liability_type, as_of_date,
			last_statement_balance, minimum_payment_amount, details)
		VALUES ($1, $2, 'credit', $3, 431.55, 25.00, '{}'::jsonb)
		RETURNING id
	`, f.UserID, f.AccountID, asOf).Scan(&id), "seed liability_statement")
	return id
}

// seedBalanceSnapshotAt inserts a balance snapshot at an explicit instant and value. Values and
// instants are both supplied by the test: nothing here compares a Go timestamp to a DB-generated
// one, because the Docker Postgres clock runs about a millisecond behind the host.
func seedBalanceSnapshotAt(ctx context.Context, t *testing.T, db DB, userID, accountID, userAccountID uuid.UUID, at time.Time, balance float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, user_account_id, current_balance,
			currency_code, snapshot_at, source)
		VALUES ($1, $2, $3, $4, 'USD', $5, 'manual')
		RETURNING id
	`, accountID, userID, userAccountID, balance, at).Scan(&id), "seed balance_snapshot")
	return id
}

// TestHistoryRetentionRoundTrip_DisconnectKeepsEveryKindOfHistory is scenario 1.
//
// The failure it exists to catch is the one #1569 was filed for: the user presses "Disconnect",
// meaning "stop syncing", and two years of financial history goes with it. Every one of the four
// kinds of history is asserted separately, because they are destroyed by four different mechanisms
// (an explicit DELETE, an FK cascade, an FK cascade, and a sync-time replace) and a test that
// checked only one would pass while the other three were lost.
func TestHistoryRetentionRoundTrip_DisconnectKeepsEveryKindOfHistory(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "rt-disconnect-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	lastMonth := time.Now().UTC().AddDate(0, 0, -30)
	balanceID := seedBalanceSnapshotAt(ctx, t, pool, f.UserID, f.AccountID, f.UserAccountID, lastMonth, 1234.56)
	txnID := seedBankingTxn(ctx, t, pool, f, lastMonth, "Grocery Store")
	holdingSnapID := seedRoundTripHoldingSnapshot(ctx, t, pool, f, lastMonth)
	statementID := seedLiabilityStatement(ctx, t, pool, f, lastMonth)

	// Sanity BEFORE the act, so that no assertion below can pass because a seed silently failed.
	require.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", balanceID))
	require.Equal(t, 1, countRows(ctx, t, pool, "transactions", "id", txnID))
	require.Equal(t, 1, countRows(ctx, t, pool, "holding_snapshots", "id", holdingSnapID))
	require.Equal(t, 1, countRows(ctx, t, pool, "liability_statements", "id", statementID))
	historyBefore := countRows(ctx, t, pool, "account_status_history", "account_id", f.AccountID)
	require.Positive(t, historyBefore, "the 000152 trigger must have recorded the account going live")

	// THE ACT: the production institution-level disconnect.
	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, f.LinkedItemID.String()))

	var status string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, f.AccountID).Scan(&status, &removedAt),
		"the account row itself must survive a disconnect — this is not a delete")
	assert.Equal(t, "removed", status)
	assert.NotNil(t, removedAt, "removed_at is what every as-of read resolves liveness against")

	assert.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", balanceID),
		"balance history must survive a disconnect")
	assert.Equal(t, 1, countRows(ctx, t, pool, "transactions", "id", txnID),
		"transactions must survive a disconnect")
	assert.Equal(t, 1, countRows(ctx, t, pool, "holding_snapshots", "id", holdingSnapID),
		"holding history must survive a disconnect — Plaid does not backfill positions")
	assert.Equal(t, 1, countRows(ctx, t, pool, "liability_statements", "id", statementID),
		"captured statements must survive a disconnect — the provider only reports the current one")
	assert.GreaterOrEqual(t, countRows(ctx, t, pool, "account_status_history", "account_id", f.AccountID),
		historyBefore+1, "the disconnect itself must be recorded as a status change, not just applied")

	// And the history must still be READABLE, not merely present: an aggregate that resolves its
	// account universe from accounts.status today would return nothing here.
	points, err := NewBalanceSnapshotRepository(pool).GetAggregatedHistory(ctx, f.UserID.String(),
		lastMonth.AddDate(0, 0, -1), time.Now().UTC(), nil)
	require.NoError(t, err)
	require.Len(t, points, 1, "the retained day must still be readable after the disconnect")
	assert.InDelta(t, 1234.56, points[0].TotalBalance, 0.01)
}

// TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting is scenario 2.
//
// Re-linking is the case where the two failure modes point in OPPOSITE directions, which is why
// both are asserted in one test. Drop the old account from the universe and last month vanishes;
// keep it naively and the overlap day counts the same money twice, so the user's net worth appears
// to jump the day they changed banks. The dedup key is
// COALESCE(user_account_id, account_id) — the re-link chain — and both halves fall out of it.
func TestHistoryRetentionRoundTrip_InstitutionChangeKeepsLastMonthWithoutDoubleCounting(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))
	snapshots := NewBalanceSnapshotRepository(pool)

	prefix := "rt-relink-" + uuid.New().String()
	f := seedRetainedFixture(ctx, t, pool, prefix)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	now := time.Now().UTC()
	lastMonthDay := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	switchDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2)

	// The old institution: a month of history, and a morning snapshot on the switch day.
	seedBalanceSnapshotAt(ctx, t, pool, f.UserID, f.AccountID, f.UserAccountID, lastMonthDay, 800.00)
	seedBalanceSnapshotAt(ctx, t, pool, f.UserID, f.AccountID, f.UserAccountID, switchDay.Add(8*time.Hour), 800.00)

	// The new institution: a new linked_item and a new account, sharing the SAME user_account_id.
	// That shared key is what makes the two rows one account historically.
	newItemID := uuid.New()
	newAccountID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, newItemID, f.UserID, prefix+"-item2-"+newItemID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'New Bank Checking', 'depository', 'active', $5)`,
		newAccountID, newItemID, f.UserID, prefix+"-acct2-"+newAccountID.String(), f.UserAccountID)
	require.NoError(t, err)

	// THE ACT: the production re-link transition, old -> new.
	newIDStr := newAccountID.String()
	require.NoError(t, accountRepo.UpdateStatus(ctx, f.AccountID.String(), f.UserID.String(),
		domain.AccountStatusReplaced, &newIDStr))

	// The new account reports the same money later on the same day.
	seedBalanceSnapshotAt(ctx, t, pool, f.UserID, newAccountID, f.UserAccountID, switchDay.Add(20*time.Hour), 800.00)

	points, err := snapshots.GetAggregatedHistory(ctx, f.UserID.String(),
		lastMonthDay.AddDate(0, 0, -1), now, nil)
	require.NoError(t, err)

	byDay := map[string]float64{}
	for _, p := range points {
		byDay[p.Date.Format("2006-01-02")] = p.TotalBalance
	}

	lastMonthKey := lastMonthDay.Format("2006-01-02")
	require.Contains(t, byDay, lastMonthKey,
		"last month's history belongs to the OLD account, which is now 'replaced'; if it is gone, the "+
			"read is resolving its universe from the current status instead of the recorded chain")
	assert.InDelta(t, 800.00, byDay[lastMonthKey], 0.01)

	switchKey := switchDay.Format("2006-01-02")
	require.Contains(t, byDay, switchKey, "the switch day must be present")
	assert.InDelta(t, 800.00, byDay[switchKey], 0.01,
		"1600 here means the overlap day was double-counted: both the old and the new account "+
			"contributed, and the user's net worth appears to double the day they changed banks")
}

// TestHistoryRetentionRoundTrip_PurgeDestroysOnlyWhatItNames is scenario 3.
//
// The purge is the ONE path allowed to destroy history, which makes its blast radius the thing to
// pin: a purge that takes a sibling account's history with it is the same defect as a disconnect
// that takes its own. The second account here exists only to be untouched.
func TestHistoryRetentionRoundTrip_PurgeDestroysOnlyWhatItNames(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	prefix := "rt-purge-" + uuid.New().String()
	f := seedRetainedFixture(ctx, t, pool, prefix)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	lastMonth := time.Now().UTC().AddDate(0, 0, -30)
	balanceID := seedBalanceSnapshotAt(ctx, t, pool, f.UserID, f.AccountID, f.UserAccountID, lastMonth, 100.00)
	holdingSnapID := seedRoundTripHoldingSnapshot(ctx, t, pool, f, lastMonth)
	statementID := seedLiabilityStatement(ctx, t, pool, f, lastMonth)
	txnID := seedBankingTxn(ctx, t, pool, f, lastMonth, "Bookshop")

	// A sibling account under a second institution, with history of its own.
	siblingItemID := uuid.New()
	siblingAccountID := uuid.New()
	siblingUserAccountID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, siblingItemID, f.UserID, prefix+"-sib-item-"+siblingItemID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, siblingUserAccountID, f.UserID, prefix+"-sib-inst")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Sibling Savings', 'depository', 'active', $5)`,
		siblingAccountID, siblingItemID, f.UserID, prefix+"-sib-acct-"+siblingAccountID.String(), siblingUserAccountID)
	require.NoError(t, err)
	siblingBalanceID := seedBalanceSnapshotAt(ctx, t, pool, f.UserID, siblingAccountID, siblingUserAccountID, lastMonth, 55.00)

	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, f.LinkedItemID.String()))

	// THE ACT: the one path that is allowed to destroy history.
	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil))

	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", f.AccountID), "the purged account is gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "balance_snapshots", "id", balanceID),
		"the purged account's balance history is gone — this is what the user asked for")
	assert.Equal(t, 0, countRows(ctx, t, pool, "holding_snapshots", "id", holdingSnapID))
	assert.Equal(t, 0, countRows(ctx, t, pool, "liability_statements", "id", statementID))
	assert.Equal(t, 0, countRows(ctx, t, pool, "account_status_history", "account_id", f.AccountID))

	// ...and nothing else. A banking transaction outlives its account (ON DELETE SET NULL, migration
	// 000035) because the user categorised and budgeted against it.
	var accountIDAfter *uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT account_id FROM transactions WHERE id = $1`, txnID).Scan(&accountIDAfter),
		"the transaction must still exist")
	assert.Nil(t, accountIDAfter, "it survives orphaned, not deleted")

	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", siblingAccountID),
		"purging one institution must not touch another")
	assert.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", siblingBalanceID),
		"the sibling institution's history must be untouched")
}
