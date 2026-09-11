//go:build integration

package repository

// Issue #1609, Item 5 — a brokerage cash-management account that Plaid reports through BOTH
// /transactions/sync and /investments/transactions/get shows every real movement TWICE in the
// unified activity feed. `user_accounts.exclude_investment_sync` (migration 000187) is the
// per-chain opt-out, and it must be honoured by BOTH read surfaces:
//
//   1. the unified activity feed          — transactionRepository.GetByUserIDKeysetUnified
//   2. the direct investments endpoint    — investmentTransactionRepository.GetByUserIDKeyset
//      (GET /api/v1/investments/investment-transactions, consumed directly by the frontend)
//
// Filtering only (1) leaves the investments page still showing the rows the accounts page just
// hid, so every behavioural case below is asserted against BOTH.
//
// ⚠ These are integration tests on purpose. pgxmock asserts the SQL *string* and then returns rows
// the test author invented, so it cannot catch a wrong join, a wrong NULL predicate, or a fan-out
// — which are precisely the three ways this change can go wrong.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// excludeSyncFixture is one user owning one chain, one investment account on that chain, one
// investment transaction and one banking transaction on the SAME account.
//
// The banking row is not scenery: the whole point of the flag is that it hides the DUPLICATE
// (the investment row) and leaves the REAL movement (the banking row) alone. A filter that hid
// both would "fix" the double-count by deleting the user's transaction from view.
type excludeSyncFixture struct {
	userID        uuid.UUID
	userAccountID uuid.UUID
	accountID     uuid.UUID
	secID         uuid.UUID
	invTxnID      uuid.UUID
	bankTxnID     uuid.UUID
}

func seedExcludeSyncFixture(ctx context.Context, t *testing.T, tx pgx.Tx, prefix string) excludeSyncFixture {
	t.Helper()

	userID, linkedItemID, accountID, secID := seedInvestmentFixture(ctx, t, tx, prefix)

	f := excludeSyncFixture{
		userID:        userID,
		userAccountID: uuid.New(),
		accountID:     accountID,
		secID:         secID,
		invTxnID:      uuid.New(),
		bankTxnID:     uuid.New(),
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, $3)
	`, f.userAccountID, f.userID, "ins-"+prefix+"-"+f.userAccountID.String())
	require.NoError(t, err, "seeding the chain")

	// Put the physical account ON the chain — this is what makes the flag reachable.
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		f.userAccountID, f.accountID)
	require.NoError(t, err, "chaining the account")

	f.seedInvestmentTxn(ctx, t, tx, f.invTxnID, &f.userAccountID, prefix)

	ext := "plaid-bank-" + prefix + "-" + f.bankTxnID.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, user_account_id,
			 source, external_transaction_id, external_transaction_id_blind_idx,
			 name, amount, date, pending, category_source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'plaid', $6, $7,
			'Automated Payment CITI AUTOPAY', 1431.48, '2026-03-15', false, 'plaid', now(), now())
	`, f.bankTxnID, f.userID, f.accountID, linkedItemID, f.userAccountID, ext, ext)
	require.NoError(t, err, "seeding the real banking row")

	return f
}

// seedInvestmentTxn inserts one investment transaction, with the caller's choice of
// user_account_id — nil is the LEGACY shape (the column has been nullable since 000071).
func (f excludeSyncFixture) seedInvestmentTxn(
	ctx context.Context, t *testing.T, tx pgx.Tx, id uuid.UUID, userAccountID *uuid.UUID, prefix string,
) {
	t.Helper()
	_, err := tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, user_account_id, security_id, plaid_investment_transaction_id,
			 type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, $6, 'cash', 'CUR:USD · U S Dollar', 1431.48, '2026-03-15')
	`, id, f.userID, f.accountID, userAccountID, f.secID, "plaid-inv-"+prefix+"-"+id.String())
	require.NoError(t, err, "seeding investment transaction %s", id)
}

func setExcludeInvestmentSync(ctx context.Context, t *testing.T, tx pgx.Tx, userAccountID uuid.UUID, v bool) {
	t.Helper()
	ct, err := tx.Exec(ctx,
		`UPDATE user_accounts SET exclude_investment_sync = $1 WHERE id = $2`, v, userAccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the flag must land on exactly one chain row")
}

// feedIDs runs the unified activity feed and returns the ids it returned, keyed by source, so an
// assertion can say "the investment row is gone AND the banking row is not" in one place.
func feedIDs(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID) (invIDs, bankIDs []string) {
	t.Helper()
	repo := NewTransactionRepository(tx, newTestFieldEncryptor(t))
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
	}, nil, 50)
	require.NoError(t, err, "the unified feed query must not error")
	for _, it := range items {
		if it.Source == domain.FeedSourceInvestment {
			invIDs = append(invIDs, it.ID)
		} else {
			bankIDs = append(bankIDs, it.ID)
		}
	}
	return
}

// directIDs runs the direct investments endpoint's repository method.
func directIDs(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID) []string {
	t.Helper()
	repo := NewInvestmentTransactionRepository(tx)
	txns, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), domain.InvestmentTransactionFilters{}, nil, 50)
	require.NoError(t, err, "the direct investments query must not error")
	ids := make([]string, 0, len(txns))
	for _, txn := range txns {
		ids = append(ids, txn.ID)
	}
	return ids
}

func beginExcludeSyncTx(ctx context.Context, t *testing.T) pgx.Tx {
	t.Helper()
	pool := connectTestDB(t)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func uniquePrefix(name string) string {
	return fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
}

// ─────────────────────────────────────────────────────────────────────────────
// Cases 1 & 3 — the flag hides the investment rows on BOTH surfaces, and hides
//               nothing else.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_HidesInvestmentRowsFromBothSurfaces(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	f := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-on"))

	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, true)

	invIDs, bankIDs := feedIDs(ctx, t, tx, f.userID)
	require.NotContains(t, invIDs, f.invTxnID.String(),
		"the unified feed still returns the duplicate investment row for a chain flagged "+
			"exclude_investment_sync — the investment branch of the UNION ALL is not filtered")
	require.Contains(t, bankIDs, f.bankTxnID.String(),
		"the flag hid the user's REAL banking transaction. It must suppress ONLY the investment "+
			"branch; hiding the banking row turns a double-count into data loss")

	require.NotContains(t, directIDs(ctx, t, tx, f.userID), f.invTxnID.String(),
		"GET /api/v1/investments/investment-transactions still returns the row the accounts page "+
			"just hid — filtering only the unified feed leaves the investments page inconsistent")
}

// ─────────────────────────────────────────────────────────────────────────────
// Cases 2 & 4 — the default (FALSE) changes nothing. The no-regression half.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_DefaultFalseKeepsEverythingVisible(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	f := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-off"))

	var flag bool
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT exclude_investment_sync FROM user_accounts WHERE id = $1`, f.userAccountID).Scan(&flag))
	require.False(t, flag, "the column's default must be FALSE, so this migration changes nothing observable")

	invIDs, bankIDs := feedIDs(ctx, t, tx, f.userID)
	require.Contains(t, invIDs, f.invTxnID.String(), "an unflagged chain's investment rows must stay in the feed")
	require.Contains(t, bankIDs, f.bankTxnID.String(), "an unflagged chain's banking rows must stay in the feed")
	require.Contains(t, directIDs(ctx, t, tx, f.userID), f.invTxnID.String(),
		"an unflagged chain's investment rows must stay on the direct endpoint")
}

// ─────────────────────────────────────────────────────────────────────────────
// Case 5 — THE DATA-LOSS GUARD.
//
// investment_transactions.user_account_id is nullable (000071), and a great many legacy rows
// carry NULL. A naive `JOIN user_accounts ... WHERE NOT exclude_investment_sync` drops EVERY one
// of them from both surfaces — a far worse bug than the double-count it was written to fix, and
// one that looks like the user's history was deleted.
//
// The row seeded here is on ANOTHER user's-account-less account, so no chain can attribute it,
// while a DIFFERENT chain in the same user's data carries the flag.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_LegacyNullUserAccountIDStaysVisible(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	prefix := uniquePrefix("excl-null")
	f := seedExcludeSyncFixture(ctx, t, tx, prefix)

	// A second, UNCHAINED account for the same user, carrying a legacy NULL-attribution row.
	legacyAccountID := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		SELECT $1, a.linked_item_id, a.user_id, $2, 'Legacy Brokerage', 'investment', 'active'
		FROM accounts a WHERE a.id = $3
	`, legacyAccountID, "legacy-acct-"+prefix, f.accountID)
	require.NoError(t, err, "seeding the unchained legacy account")

	legacyTxnID := uuid.New()
	legacy := f
	legacy.accountID = legacyAccountID
	legacy.seedInvestmentTxn(ctx, t, tx, legacyTxnID, nil, prefix+"-legacy")

	// The OTHER chain is flagged. The legacy row belongs to no chain at all.
	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, true)

	invIDs, _ := feedIDs(ctx, t, tx, f.userID)
	require.Contains(t, invIDs, legacyTxnID.String(),
		"a legacy investment row with a NULL user_account_id vanished from the unified feed. The "+
			"exclusion predicate must be NULL-tolerant (LEFT JOIN / NOT EXISTS), never an inner "+
			"join onto user_accounts")
	require.Contains(t, directIDs(ctx, t, tx, f.userID), legacyTxnID.String(),
		"a legacy investment row with a NULL user_account_id vanished from the direct investments "+
			"endpoint — same NULL-tolerance requirement")
}

// A legacy NULL row sitting on an account that IS on the flagged chain is a different question,
// and the answer must be "excluded": every other chain-expanding read in this package attributes a
// NULL user_account_id row through accounts.user_account_id for its account_id
// (investment_transaction_repository.go's GetByAccountID, and the unified feed's own account
// filter). If this read alone declined to, the reported defect would survive the fix for exactly
// the accounts whose rows predate the 000071 backfill.
func TestExcludeInvestmentSync_LegacyNullRowOnTheFlaggedAccountIsStillExcluded(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	prefix := uniquePrefix("excl-null-on-chain")
	f := seedExcludeSyncFixture(ctx, t, tx, prefix)

	legacyTxnID := uuid.New()
	f.seedInvestmentTxn(ctx, t, tx, legacyTxnID, nil, prefix+"-legacy")

	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, true)

	invIDs, bankIDs := feedIDs(ctx, t, tx, f.userID)
	require.NotContains(t, invIDs, legacyTxnID.String(),
		"a NULL-attribution row on the flagged account must be attributed through "+
			"accounts.user_account_id, exactly as every other chain-expanding read does")
	require.Contains(t, bankIDs, f.bankTxnID.String(), "the banking row is still not the target")
	require.NotContains(t, directIDs(ctx, t, tx, f.userID), legacyTxnID.String(),
		"same attribution on the direct endpoint")
}

// ─────────────────────────────────────────────────────────────────────────────
// Case 6 — RETENTION OF ROWS ALREADY STORED. Setting the flag destroys nothing, so
//          clearing it brings back every row that was written while it was off.
//
// ⚠ THIS IS NARROWER THAN IT LOOKS, AND ITS OLD COMMENT WAS FALSIFIED BY #1631 M4.
//   It used to read: "clearing the flag brings back exactly the same rows... the
//   property that makes a read filter the right answer rather than an ingestion
//   filter". #1631 M4 made the flag an ingestion filter TOO -- the sync no longer
//   persists new investment_transactions for an excluded chain -- and this test
//   STAYED GREEN, because it seeds its rows directly with INSERT and never runs a
//   sync. That is precisely the silently-falsified-test-comment failure mode
//   commit 832f40fc exists to punish, so the comment is corrected here rather than
//   the test being deleted: what it pins is still true and still worth pinning.
//
//   What it proves: the read filter DESTROYS NOTHING. A row present before the
//   flag was set is retained, is still in the table, and is served again the
//   moment the flag is cleared -- on both read surfaces.
//   What it does NOT prove, and must not be cited for: that the flag is fully
//   reversible. Rows the sync declined to INGEST while the flag was set do not
//   exist to come back, and the backfill that fetches them reaches only
//   investmentTxnLookbackYears (2 years). See
//   docs/decisions/2026-08-24-financial-history-retained-by-default.md
//   section "AMENDMENT (#1631, 2026-09-10): a fourth category -- never ingesting a row".
//
//   ⚠ Do NOT rename this test to match the narrower claim: transaction_repository.go's
//   investmentSyncExclusionCondition cites it BY NAME, and TestEveryCitedTestNameResolves
//   fails on a dangling citation.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_IsReversible(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	f := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-rev"))

	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, true)
	invIDs, _ := feedIDs(ctx, t, tx, f.userID)
	require.NotContains(t, invIDs, f.invTxnID.String(), "precondition: the flag hides the row")

	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, false)

	invIDs, bankIDs := feedIDs(ctx, t, tx, f.userID)
	require.Contains(t, invIDs, f.invTxnID.String(),
		"clearing the flag did not bring back a row that was ALREADY STORED when the flag was "+
			"set. The read filter must destroy and suppress nothing — if a stored row does not "+
			"reappear, something deleted or tombstoned it, which the retention ADR forbids. "+
			"(This says nothing about rows the sync declined to ingest while the flag was set; "+
			"those never existed, and the backfill for them is bounded at 2 years.)")
	require.Contains(t, bankIDs, f.bankTxnID.String(), "the banking row was never affected")
	require.Contains(t, directIDs(ctx, t, tx, f.userID), f.invTxnID.String(),
		"the direct endpoint must retain and re-serve the stored row too")

	var stillThere int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM investment_transactions WHERE id = $1`, f.invTxnID).Scan(&stillThere))
	require.Equal(t, 1, stillThere,
		"the underlying row must never have been deleted — hiding is a read concern (retention ADR)")
}

// ─────────────────────────────────────────────────────────────────────────────
// Case 7 — FAN-OUT. One chain has MANY accounts rows (that is what a re-link chain
//          IS). A JOIN onto user_accounts through accounts would multiply each feed
//          row by the number of chain members. NOT EXISTS is a semi-join and cannot.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_MultipleAccountsOnOneChainDoNotFanOut(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	prefix := uniquePrefix("excl-fanout")
	f := seedExcludeSyncFixture(ctx, t, tx, prefix)

	// Two more physical accounts on the SAME chain — the re-link history.
	for i := 0; i < 2; i++ {
		sibling := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
			SELECT $1, a.linked_item_id, a.user_id, $2, 'Re-linked Brokerage', 'investment', 'active', a.user_account_id
			FROM accounts a WHERE a.id = $3
		`, sibling, fmt.Sprintf("sibling-%d-%s", i, prefix), f.accountID)
		require.NoError(t, err, "seeding chain sibling %d", i)
	}

	// Unflagged first: the row must appear EXACTLY ONCE, not once per chain member.
	invIDs, bankIDs := feedIDs(ctx, t, tx, f.userID)
	require.Equal(t, []string{f.invTxnID.String()}, invIDs,
		"the investment row was duplicated once per account on the chain — the exclusion predicate "+
			"fanned out. It must be a semi-join (NOT EXISTS), not a JOIN")
	require.Equal(t, []string{f.bankTxnID.String()}, bankIDs, "the banking branch must not fan out either")
	require.Equal(t, []string{f.invTxnID.String()}, directIDs(ctx, t, tx, f.userID),
		"the direct endpoint must not fan out either")

	// Flagged: setting it ONCE on the chain covers every member.
	setExcludeInvestmentSync(ctx, t, tx, f.userAccountID, true)
	invIDs, bankIDs = feedIDs(ctx, t, tx, f.userID)
	require.Empty(t, invIDs, "one flag on the chain must cover every account on it")
	require.Equal(t, []string{f.bankTxnID.String()}, bankIDs, "and must still leave banking alone")
	require.Empty(t, directIDs(ctx, t, tx, f.userID), "same on the direct endpoint")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tenancy — another user's flag must not hide this user's rows, and vice versa.
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_IsScopedToItsOwnChain(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	mine := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-mine"))
	theirs := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-theirs"))

	setExcludeInvestmentSync(ctx, t, tx, theirs.userAccountID, true)

	invIDs, _ := feedIDs(ctx, t, tx, mine.userID)
	require.Contains(t, invIDs, mine.invTxnID.String(),
		"another user's exclude_investment_sync flag hid this user's investment row — the predicate "+
			"must key on the row's OWN chain")
	require.Contains(t, directIDs(ctx, t, tx, mine.userID), mine.invTxnID.String(),
		"same on the direct endpoint")
}

// ─────────────────────────────────────────────────────────────────────────────
// Write-path round trip — the flag survives the repository, and every read of a
// user_account carries it.
//
// The service tests assert the *bool is applied to the domain object; only a real
// round trip proves the column made it into the UPDATE's SET list AND into every
// SELECT's column list. A field left out of the SELECT reads back as FALSE, which
// looks exactly like "the user never set it".
// ─────────────────────────────────────────────────────────────────────────────

func TestExcludeInvestmentSync_SurvivesTheUserAccountRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx := beginExcludeSyncTx(ctx, t)
	f := seedExcludeSyncFixture(ctx, t, tx, uniquePrefix("excl-roundtrip"))

	repo := NewUserAccountRepository(tx, newTestFieldEncryptor(t))

	fetched, err := repo.GetByIDAndUser(ctx, f.userAccountID.String(), f.userID.String())
	require.NoError(t, err)
	require.False(t, fetched.ExcludeInvestmentSync, "a freshly minted chain must not be excluded")

	fetched.ExcludeInvestmentSync = true
	require.NoError(t, repo.Update(ctx, fetched))

	reread, err := repo.GetByIDAndUser(ctx, f.userAccountID.String(), f.userID.String())
	require.NoError(t, err)
	require.True(t, reread.ExcludeInvestmentSync,
		"exclude_investment_sync did not survive Update+re-read — it is missing from the UPDATE's "+
			"SET list or from the SELECT's column list")

	// And the flag the repository just wrote is the one the feed reads.
	invIDs, _ := feedIDs(ctx, t, tx, f.userID)
	require.NotContains(t, invIDs, f.invTxnID.String(),
		"the write path and the read filter must agree on the same column")

	listed, err := repo.GetByUserID(ctx, f.userID.String())
	require.NoError(t, err)
	var seen bool
	for _, ua := range listed {
		if ua.ID == f.userAccountID.String() {
			seen = true
			require.True(t, ua.ExcludeInvestmentSync,
				"the list read (GET /api/v1/user-accounts) must carry the flag too, or no UI can "+
					"ever show its state")
		}
	}
	require.True(t, seen, "the seeded chain must appear in the list read")
}
