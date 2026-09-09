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
// WITHIN the list, which is the half that can drift silently. Nothing yet compares a Sankey node to
// a list total.
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
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "totals-item-"+linkedItemID.String())

	mustExec("account active", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Active','depository','active')`,
		activeAcct, linkedItemID, userID, userAccountID, "plaid-"+activeAcct.String())
	// The gap-1 shape. The insert trigger writes one OPEN 'removed' interval and no 'active' row,
	// so this account is visible only through accountWithNoActiveIntervalLeg.
	mustExec("account terminal-only", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status, removed_at, removed_reason)
		VALUES ($1,$2,$3,$4,$5,'Terminal Only','depository','removed', now(), 'user_disconnect')`,
		terminalOnly, linkedItemID, userID, userAccountID, "plaid-"+terminalOnly.String())

	// Amounts are distinct and non-round so a total assembled from the wrong subset cannot coincide
	// with the right one, and PFCv2 signs are mixed so a SUM that lost its sign would show up.
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
			id, userID, accountID, userAccountID, linkedItemID,
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

	// The whole point is that the page is SMALLER than the set: with limit >= the set size, a total
	// derived from the returned page would agree by accident and the test would prove nothing.
	const pageLimit = 3

	cases := []struct {
		name    string
		filters domain.TransactionFilters
	}{
		{"no filters", domain.TransactionFilters{}},
		{"uncategorized only", domain.TransactionFilters{Uncategorized: true}},
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

				// No investment rows are seeded, so the unified set is the banking set: the two paths
				// must agree with each other as well as with the rows. They are separate queries with
				// separately assembled predicates, so this is a real cross-check, not a tautology.
				assert.Equal(t, wantCount, totals.Count,
					"the unified aggregate must admit the same rows as the banking one")
				assert.InDelta(t, wantAmount, totals.Amount, 0.001,
					"the unified aggregate must sum the same rows as the banking one")
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
