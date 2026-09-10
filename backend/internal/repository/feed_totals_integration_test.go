//go:build integration

package repository

// Issue #1626 Phase 2, gap 2 — behavioural proof that the whole-set totals AGREE with the rows they
// describe.
//
// WHY THIS TEST IS THE POINT OF THE FEATURE. The reports Sankey claims that clicking a node lands
// on a list that accounts for the node's amount. Before this work the response carried no total at
// all, so that claim was not merely untested — it was unverifiable through the product, and the
// verification step written for it could not be executed by anyone.
//
// ⚠ "ACCOUNTS FOR", not "equals". A hierarchy node's amount is COALESCE(SUM(ABS(t.amount)),0) — an
// unsigned chart quantity — while total_amount is SUM(t.amount) under PFCv2 signs, so the property
// is |node amount| == |total_amount|: equal for an expense node, off by a factor of −1 for an income
// one. This file does NOT test that cross-surface property; it tests the aggregate against the rows
// WITHIN the list, which is the half that can drift silently. The cross-surface half is
// TestSankeyClickThrough_NodeAmountAndListTotalDescribeTheSameSet_Integration (#1626 gap G4), which
// calls both repositories in one fixture; until it existed nothing compared a node to a list total.
//
// WHY IT CANNOT BE A pgxmock TEST. The correctness argument for the aggregate is "it uses the same
// predicate as the page query", and the two implementations honour that by reusing the page query's
// own WHERE fragment and arg slice verbatim. But two SQL strings matching a regex is not two result
// sets agreeing: a fragment could be interpolated at the wrong position, a $N could shift, a UNION
// arm could be dropped, and every shape assertion in feed_totals_test.go would still pass. Only
// Postgres can compare the aggregate to the rows. This is the only place the drift the comments warn
// about would actually be caught.
//
// The fixture deliberately includes the account shape that gap 1 fixed — one whose status history
// has NO 'active' interval — because that was the one shape where the hierarchy read and the list
// disagreed. A total computed over a different visibility universe than the rows would show up here
// as a count mismatch and nowhere else.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestFeedTotals_AgreeWithTheRowsTheyDescribe_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewTransactionRepository(tx, newTestFieldEncryptor(t))

	userID := uuid.New()
	userAccountID := uuid.New()
	// ⚠ THE TERMINAL-ONLY ACCOUNT NEEDS ITS OWN CHAIN KEY, and it did not have one until #1626 gap
	// G4's mutation testing. Sharing userAccountID with the ACTIVE account made
	// activeChainSiblingLeg admit its rows — that leg fires whenever any active account shares the
	// row's user_account_id — so the comment below claiming they are visible "only through
	// accountWithNoActiveIntervalLeg" was false, and DELETING that leg left this file green.
	terminalUserAccountID := uuid.New()
	linkedItemID := uuid.New()
	activeAcct := uuid.New()
	terminalOnly := uuid.New()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`, userID, "test|"+userID.String(), "totals-1626-"+userID.String()+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-totals-1626','depository')`, userAccountID, userID)
	mustExec("terminal user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-totals-1626-terminal','depository')`, terminalUserAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "totals-item-"+linkedItemID.String())

	mustExec("account active", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Active','depository','active')`,
		activeAcct, linkedItemID, userID, userAccountID, "plaid-"+activeAcct.String())
	// The gap-1 shape. The insert trigger writes one OPEN 'removed' interval and no 'active' row,
	// and it is on its OWN chain key with no active sibling, so its rows satisfy neither
	// activeChainSiblingLeg nor retainedAccountNoLiveSuccessorLeg (which needs an 'active' history
	// row): accountWithNoActiveIntervalLeg is the only thing that admits them. Verified by deleting
	// that leg, which now reddens this file.
	mustExec("account terminal-only", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status, removed_at, removed_reason)
		VALUES ($1,$2,$3,$4,$5,'Terminal Only','depository','removed', now(), 'user_disconnect')`,
		terminalOnly, linkedItemID, userID, terminalUserAccountID, "plaid-"+terminalOnly.String())

	// Amounts are distinct and non-round so a total assembled from the wrong subset cannot coincide
	// with the right one, and PFCv2 signs are mixed so a SUM that lost its sign would show up.
	// chainKeyOf pairs a row with its own account's chain key. A row carrying the ACTIVE account's
	// user_account_id would be admitted by activeChainSiblingLeg regardless of its own account's
	// status, which is exactly how this fixture used to pass with the retained leg deleted.
	chainKeyOf := func(accountID uuid.UUID) uuid.UUID {
		if accountID == terminalOnly {
			return terminalUserAccountID
		}
		return userAccountID
	}

	seed := func(accountID uuid.UUID, amount float64, uncategorized bool, name string) {
		id := uuid.New()
		pfc := "FOOD_AND_DRINK"
		var pfcArg any = pfc
		if uncategorized {
			pfcArg = nil
		}
		mustExec("txn "+name, `
			INSERT INTO transactions
				(id, user_id, account_id, user_account_id, linked_item_id, source,
				 external_transaction_id, external_transaction_id_blind_idx,
				 amount, date, name, pending, pfc_primary, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'plaid',$6,$7,$8, CURRENT_DATE - 1, $9, false, $10, now(), now())`,
			id, userID, accountID, chainKeyOf(accountID), linkedItemID,
			"ext-"+id.String(), "blind-"+id.String(), amount, name, pfcArg)
	}

	// Seven rows: four uncategorized (two of them on the terminal-only account), three categorised.
	seed(activeAcct, 11.11, true, "u1")
	seed(activeAcct, 22.22, true, "u2")
	seed(terminalOnly, 33.33, true, "u3")
	seed(terminalOnly, -44.44, true, "u4-inflow")
	seed(activeAcct, 55.55, false, "c1")
	seed(activeAcct, 66.66, false, "c2")
	seed(terminalOnly, 77.77, false, "c3")

	// ── The investment UNION arm (#1626 gap G14) ──────────────────────────────────────────────
	//
	// The unified subtest below sets IncludeInvestmentActivity=true, and until this fixture existed
	// it seeded NO investment rows: the second arm of both the page query and the aggregate
	// contributed nothing, so the subtest's name and its "the two paths must agree" comment
	// described coverage it did not have. Dropping the investment arm from the aggregate entirely
	// would have left it green.
	//
	// Signs are MIXED and no amount is round, so an aggregate that summed magnitudes, or that
	// picked up the arm's rows twice, cannot coincide with the right answer. security_id is left
	// NULL: the union LEFT JOINs securities, so a security row would add fixture surface without
	// adding a predicate.
	seedInvestment := func(accountID uuid.UUID, amount float64, tradeType, name string) {
		id := uuid.New()
		mustExec("inv txn "+name, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, user_account_id, plaid_investment_transaction_id,
				 type, name, amount, date)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8, CURRENT_DATE - 1)`,
			id, userID, accountID, chainKeyOf(accountID), "plaid-inv-"+id.String(), tradeType, name, amount)
	}
	// The third row sits on the terminal-only account, so the investment arm's own copy of
	// retainedAccountVisibilityLegs has to admit it — the arm carries the legs separately from the
	// banking arm, and this is what makes that copy load-bearing here.
	seedInvestment(activeAcct, 101.01, "buy", "i1-buy")
	seedInvestment(activeAcct, -202.02, "sell", "i2-sell")
	seedInvestment(terminalOnly, 303.03, "buy", "i3-terminal-only")
	const (
		investmentCount = 3
		investmentSum   = 101.01 - 202.02 + 303.03
	)

	// The whole point is that the page is SMALLER than the set: with limit >= the set size, a total
	// derived from the returned page would agree by accident and the test would prove nothing.
	const pageLimit = 3

	// wantInvestmentCount/Sum is what the investment arm is expected to CONTRIBUTE to the unified
	// totals for each case. It is zero for the uncategorized case because a category filter zeroes
	// that arm (` AND FALSE`, see categoryFilterGroup's caller in GetByUserIDKeysetUnified):
	// investment rows carry no category at all, so a categorised feed must not list them. Asserting
	// the zero is as load-bearing as asserting the non-zero — a leak there would report investment
	// money under a category the user filtered to.
	cases := []struct {
		name                string
		filters             domain.TransactionFilters
		wantInvestmentCount int
		wantInvestmentSum   float64
	}{
		{"no filters", domain.TransactionFilters{}, investmentCount, investmentSum},
		{"uncategorized only", domain.TransactionFilters{Uncategorized: true}, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The reference set: every row the SAME predicate admits, fetched in full.
			all, _, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), tc.filters, nil, 100)
			require.NoError(t, err)
			wantCount := len(all)
			wantAmount := 0.0
			for _, r := range all {
				wantAmount += r.Amount
			}
			require.NotZero(t, wantCount, "fixture must admit rows, or this case asserts nothing")
			require.Greater(t, wantCount, pageLimit,
				"the reference set must exceed one page, or a page-derived total would pass")

			t.Run("banking-only path", func(t *testing.T) {
				page, _, totals, err := repo.GetByUserIDKeyset(ctx, userID.String(), tc.filters, nil, pageLimit)
				require.NoError(t, err)
				require.Len(t, page, pageLimit, "the page must be short, so totals cannot come from it")
				require.NotNil(t, totals)

				assert.Equal(t, wantCount, totals.Count,
					"the total count must describe the same set the rows are drawn from — a mismatch here "+
						"means the aggregate's predicate has drifted from the page query's")
				assert.InDelta(t, wantAmount, totals.Amount, 0.001,
					"the total amount must be the sum of exactly those rows, signs included")
			})

			t.Run("unified feed path", func(t *testing.T) {
				f := tc.filters
				f.IncludeInvestmentActivity = true
				f.SortBy = "date"
				items, _, totals, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), f, nil, pageLimit)
				require.NoError(t, err)
				require.Len(t, items, pageLimit)
				require.NotNil(t, totals)

				// The unified set is the banking set PLUS whatever the investment arm contributes for
				// this filter. Expressed as banking + contribution rather than as a literal, so the
				// assertion says which arm is being credited: an aggregate that lost the investment
				// arm collapses to wantCount and fails the "no filters" case, and one that leaked it
				// past a category filter exceeds wantCount and fails the uncategorized case.
				assert.Equal(t, wantCount+tc.wantInvestmentCount, totals.Count,
					"the unified aggregate must admit the banking rows plus this filter's investment rows")
				assert.InDelta(t, wantAmount+tc.wantInvestmentSum, totals.Amount, 0.001,
					"the unified aggregate must sum both arms with their own signs — the investment "+
						"rows include a negative one, so a magnitude sum lands elsewhere")

				// And the arm is exercised through the ROWS as well as the aggregate: an aggregate
				// agreeing with a page that never lists an investment row would prove only that two
				// queries share one mistake.
				all, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), f, nil, 100)
				require.NoError(t, err)
				investmentRows := 0
				for _, it := range all {
					if it.Source == "investment" {
						investmentRows++
					}
				}
				assert.Equal(t, tc.wantInvestmentCount, investmentRows,
					"the page query's investment arm must admit exactly the rows the aggregate counted")
			})
		})
	}

	t.Run("a cursored page reports no totals at all", func(t *testing.T) {
		page, cursor, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), domain.TransactionFilters{}, nil, pageLimit)
		require.NoError(t, err)
		require.Len(t, page, pageLimit)
		require.NotNil(t, cursor, "need a next-page cursor to exercise the skip")

		_, _, totals, err := repo.GetByUserIDKeyset(ctx, userID.String(), domain.TransactionFilters{}, cursor, pageLimit)
		require.NoError(t, err)
		assert.Nil(t, totals,
			"page two must not re-aggregate: the number does not change as the user pages, and this is "+
				"the busiest list in the app. nil is 'not computed', which the API omits rather than zeroes")
	})
}
